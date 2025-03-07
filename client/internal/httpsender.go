package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/open-telemetry/opamp-go/internal"
	"google.golang.org/protobuf/proto"

	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
)

const OpAMPPlainHTTPMethod = "POST"
const defaultPollingIntervalMs = 30 * 1000 // default interval is 30 seconds.

// HTTPSender allows scheduling messages to send. Once run, it will loop through
// a request/response cycle for each message to send and will process all received
// responses using a receivedProcessor. If there are no pending messages to send
// the HTTPSender will wait for the configured polling interval.
type HTTPSender struct {
	SenderCommon

	url               string
	logger            types.Logger
	client            *http.Client
	callbacks         types.Callbacks
	pollingIntervalMs int64

	// Headers to send with all requests.
	requestHeader http.Header

	// Processor to handle received messages.
	receiveProcessor receivedProcessor
}

func NewHTTPSender(logger types.Logger) *HTTPSender {
	h := &HTTPSender{
		SenderCommon:      NewSenderCommon(),
		logger:            logger,
		client:            http.DefaultClient,
		pollingIntervalMs: defaultPollingIntervalMs,
	}
	// initialize the headers with no additional headers
	h.SetRequestHeader(nil)
	return h
}

// Run starts the processing loop that will perform the HTTP request/response.
// When there are no more messages to send Run will suspend until either there is
// a new message to send or the polling interval elapses.
// Should not be called concurrently with itself. Can be called concurrently with
// modifying NextMessage().
// Run continues until ctx is cancelled.
func (h *HTTPSender) Run(
	ctx context.Context,
	url string,
	callbacks types.Callbacks,
	clientSyncedState *ClientSyncedState,
	packagesStateProvider types.PackagesStateProvider,
) {
	h.url = url
	h.callbacks = callbacks
	h.receiveProcessor = newReceivedProcessor(h.logger, callbacks, h, clientSyncedState, packagesStateProvider)

	for {
		pollingTimer := time.NewTimer(time.Millisecond * time.Duration(atomic.LoadInt64(&h.pollingIntervalMs)))
		select {
		case <-h.hasPendingMessage:
			// Have something to send. Stop the polling timer and send what we have.
			pollingTimer.Stop()
			h.makeOneRequestRoundtrip(ctx)

		case <-pollingTimer.C:
			// Polling interval has passed. Force a status update.
			h.NextMessage().Update(func(msg *protobufs.AgentToServer) {})
			// This will make hasPendingMessage channel readable, so we will enter
			// the case above on the next iteration of the loop.
			h.ScheduleSend()
			break

		case <-ctx.Done():
			return
		}
	}
}

// SetRequestHeader sets additional HTTP headers to send with all future requests.
// Should not be called concurrently with any other method.
func (h *HTTPSender) SetRequestHeader(header http.Header) {
	if header == nil {
		header = http.Header{}
	}
	h.requestHeader = header
	h.requestHeader.Set(headerContentType, contentTypeProtobuf)
}

func (h *HTTPSender) makeOneRequestRoundtrip(ctx context.Context) {
	resp, err := h.sendRequestWithRetries(ctx)
	if err != nil {
		return
	}
	if resp == nil {
		// No request was sent and nothing to receive.
		return
	}
	h.receiveResponse(ctx, resp)
}

func (h *HTTPSender) sendRequestWithRetries(ctx context.Context) (*http.Response, error) {
	req, err := h.prepareRequest(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			h.logger.Debugf("Client is stopped, will not try anymore.")
		} else {
			h.logger.Errorf("Failed prepare request (%v), will not try anymore.", err)
		}
		return nil, err
	}
	if req == nil {
		// Nothing to send.
		return nil, nil
	}

	// Repeatedly try requests with a backoff strategy.
	infiniteBackoff := backoff.NewExponentialBackOff()
	// Make backoff run forever.
	infiniteBackoff.MaxElapsedTime = 0

	interval := time.Duration(0)

	for {
		timer := time.NewTimer(interval)
		interval = infiniteBackoff.NextBackOff()

		select {
		case <-timer.C:
			{
				resp, err := h.client.Do(req)
				if err == nil {
					switch resp.StatusCode {
					case http.StatusOK:
						// We consider it connected if we receive 200 status from the Server.
						h.callbacks.OnConnect()
						return resp, nil

					case http.StatusTooManyRequests:
					case http.StatusServiceUnavailable:
						interval = recalculateInterval(interval, resp)
						err = fmt.Errorf("server response code=%d", resp.StatusCode)

					default:
						return nil, fmt.Errorf("invalid response from server: %d", resp.StatusCode)
					}
				} else if errors.Is(err, context.Canceled) {
					h.logger.Debugf("Client is stopped, will not try anymore.")
					return nil, err
				}

				h.logger.Errorf("Failed to do HTTP request (%v), will retry", err)
				h.callbacks.OnConnectFailed(err)
			}

		case <-ctx.Done():
			h.logger.Debugf("Client is stopped, will not try anymore.")
			return nil, ctx.Err()
		}
	}
}

func recalculateInterval(interval time.Duration, resp *http.Response) time.Duration {
	retryAfter := internal.ExtractRetryAfterHeader(resp)
	if retryAfter.Defined && retryAfter.Duration > interval {
		// If the Server suggested connecting later than our interval
		// then honour Server's request, otherwise wait at least
		// as much as we calculated.
		interval = retryAfter.Duration
	}
	return interval
}

func (h *HTTPSender) prepareRequest(ctx context.Context) (*http.Request, error) {
	msgToSend := h.nextMessage.PopPending()
	if msgToSend == nil || proto.Equal(msgToSend, &protobufs.AgentToServer{}) {
		// There is no pending message or the message is empty.
		// Nothing to send.
		return nil, nil
	}

	data, err := proto.Marshal(msgToSend)
	if err != nil {
		return nil, err
	}

	body := bytes.NewReader(data)
	req, err := http.NewRequestWithContext(ctx, OpAMPPlainHTTPMethod, h.url, body)
	if err != nil {
		return nil, err
	}

	req.Header = h.requestHeader
	return req, nil
}

func (h *HTTPSender) receiveResponse(ctx context.Context, resp *http.Response) {
	msgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		h.logger.Errorf("cannot read response body: %v", err)
		return
	}
	_ = resp.Body.Close()

	var response protobufs.ServerToAgent
	if err := proto.Unmarshal(msgBytes, &response); err != nil {
		h.logger.Errorf("cannot unmarshal response: %v", err)
		return
	}

	h.receiveProcessor.ProcessReceivedMessage(ctx, &response)
}

// SetPollingInterval sets the interval between polling. Has effect starting from the
// next polling cycle.
func (h *HTTPSender) SetPollingInterval(duration time.Duration) {
	atomic.StoreInt64(&h.pollingIntervalMs, duration.Milliseconds())
}
