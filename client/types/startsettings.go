package types

import (
	"crypto/tls"
	"net/http"

	"github.com/open-telemetry/opamp-go/protobufs"
)

type StartSettings struct {
	// Connection parameters.

	// Server URL. MUST be set.
	OpAMPServerURL string

	// Optional additional HTTP headers to send with all HTTP requests.
	Header http.Header

	// Optional TLS config for HTTP connection.
	TLSConfig *tls.Config

	// Agent information.
	InstanceUid string

	// Callbacks that the client will call after Start() returns nil.
	Callbacks Callbacks

	// Previously saved state. These will be reported to the Server immediately
	// after the connection is established.

	// The remote config status. If nil is passed it will force
	// the Server to send a remote config back. It is not required to set the Hash
	// field, it will be calculated by Start() function.
	// The Hash field will be calculated and updated from the content of the rest of
	// the fields.
	RemoteConfigStatus *protobufs.RemoteConfigStatus

	LastConnectionSettingsHash []byte

	// PackagesStateProvider provides access to the local state of packages.
	// If nil then ReportsPackageStatuses and AcceptsPackages capabilities will be disabled,
	// i.e. package status reporting and syncing from the Server will be disabled.
	PackagesStateProvider PackagesStateProvider
}
