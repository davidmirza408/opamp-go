package orionsrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/open-telemetry/opamp-go/internal/examples/server/data"
	"github.com/open-telemetry/opamp-go/protobufs"
)

var (
	logger = log.New(log.Default().Writer(), "[ORION Poller] ", log.Default().Flags()|log.Lmsgprefix|log.Lmicroseconds)

	defaultCheckInterval = 10 * time.Second

	connectionError = errors.New("OPAMP operator configuration retrieval from ORION failed")
)

type OrionPoller struct {
	apiKey               string
	principalId          string
	principalType        string
	baseURL              string
	opampOperatorAPIPath string
	interval             time.Duration
	HTTPClient           *http.Client
}

func Start() {
	orionPoller := NewOrionPoller()

	go orionPoller.runForEver()
}

func NewOrionPoller() *OrionPoller {
	orionPoller := &OrionPoller{
		apiKey:               "http",
		principalId:          "dXNlcg==",
		principalType:        "c2VydmljZQ==",
		baseURL:              "http://localhost:8084/json/v1",
		opampOperatorAPIPath: "/opamp:operator",
		interval:             defaultCheckInterval,
		HTTPClient: &http.Client{
			Timeout: defaultCheckInterval,
		},
	}

	return orionPoller
}

func (orionPoller *OrionPoller) runForEver() {
	for {
		select {
		case <-time.After(orionPoller.interval):
			orionPoller.updateOpampConfig()
		}
	}
}

func (orionPoller *OrionPoller) updateOpampConfig() {
	logger.Printf("Checking for OPAMP config update...")

	res, err := orionPoller.fetchOpampConfigFromOrion()
	if err != nil {
		logger.Printf("OPAMP config update error msg: ", err)
		return
	}

	itemsSlice := res["items"].([]interface{})
	lastItem := itemsSlice[len(itemsSlice)-1]
	lastConfigVal := lastItem.(map[string]interface{})["object"]
	// logger.Printf("Retrieved OPAMP config: ", firstConfigVal)

	lastConfigYamlBytes, err := yaml.Parser().Marshal(lastConfigVal.(map[string]interface{}))
	if err != nil {
		logger.Printf("OPAMP config YAML parse error msg: ", err)
		return
	}
	saveCustomConfigForAllInstance(lastConfigYamlBytes)
}

func (orionPoller *OrionPoller) fetchOpampConfigFromOrion() (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", orionPoller.baseURL+orionPoller.opampOperatorAPIPath, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("layer-type", "solution")
	req.Header.Set("layer-id", "opamp")

	var res map[string]interface{}
	if err := orionPoller.sendRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

// Content-type and body should be already added to req
func (orionPoller *OrionPoller) sendRequest(req *http.Request, v interface{}) error {
	req.Header.Set("Accept", "application/json; charset=utf-8")
	req.Header.Set("appd-pid", orionPoller.principalId)
	req.Header.Set("appd-pty", orionPoller.principalType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", orionPoller.apiKey))

	res, err := orionPoller.HTTPClient.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		logger.Printf("ORION connection error msg: ", res.Body)
		return connectionError
	}

	if err = json.NewDecoder(res.Body).Decode(&v); err != nil {
		return err
	}

	// logger.Printf("Retrieved OPAMP object: ", v)

	return nil
}

func saveCustomConfigForAllInstance(configBytes []byte) {
	config := &protobufs.AgentConfigMap{
		ConfigMap: map[string]*protobufs.AgentConfigFile{
			"": {Body: configBytes},
		},
	}

	data.AllAgents.SetCustomConfigForAllAgent(config)
}
