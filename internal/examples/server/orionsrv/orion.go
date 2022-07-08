package orionsrv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
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

type OrionService struct {
	orionPoller        *OrionClientInfo
	opampCurrentConfig *OpampConfiguration
}

type OrionClientInfo struct {
	apiKey               string
	principalId          string
	principalType        string
	baseURL              string
	opampOperatorAPIPath string
	interval             time.Duration
	HTTPClient           *http.Client
}

type OpampConfiguration struct {
	CurrentFullConfig string
	CurrentObjectConfig string
}

func Start() {
	// go OrionServ.runForEver()
}

func (orionService *OrionService) runForEver() {
	for {
		select {
		case <-time.After(orionService.orionPoller.interval):
			orionService.fetchOpampConfig()
		}
	}
}

func (orionService *OrionService) GetOpampConfiguration() *OpampConfiguration {
	orionService.fetchOpampConfig()

	return orionService.opampCurrentConfig
}

func (orionService *OrionService) UpdateOpampConfiguration(config string) error {
	if _, err := orionService.updateOpampConfigFromOrion(config); err != nil {
		return err
	}

	orionService.fetchOpampConfig()
	return nil
}

func (orionService *OrionService) fetchOpampConfig() {
	logger.Printf("Checking for OPAMP config update...")

	res, err := orionService.getOpampConfigFromOrion()
	if err != nil {
		logger.Printf("OPAMP config update error msg: ", err)
		return
	}

	itemsSlice := res["items"].([]interface{})
	if len(itemsSlice) > 0 {
		lastItem := itemsSlice[0]
		lastConfigVal := lastItem.(map[string]interface{})["object"]
		// logger.Printf("Retrieved OPAMP config: ", firstConfigVal)

		lastConfigYamlBytes, err := yaml.Parser().Marshal(lastConfigVal.(map[string]interface{}))
		if err != nil {
			logger.Printf("OPAMP config YAML parse error msg: ", err)
			return
		}

		saveCustomConfigForAllInstance(lastConfigYamlBytes)

		fullJsonString, err := json.MarshalIndent(lastItem.(map[string]interface{}), "", "    ")
		orionService.opampCurrentConfig.CurrentFullConfig = string(fullJsonString)

		objectJsonString, err := json.MarshalIndent(lastConfigVal.(map[string]interface{}), "", "    ")
		orionService.opampCurrentConfig.CurrentObjectConfig = string(objectJsonString)
	}
}

func (orionService *OrionService) getOpampConfigFromOrion() (map[string]interface{}, error) {
	req, err := http.NewRequest(
		"GET", orionService.orionPoller.baseURL+orionService.orionPoller.opampOperatorAPIPath, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("layer-type", "solution")
	req.Header.Set("layer-id", "opamp")

	var res map[string]interface{}
	if err := orionService.sendRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (orionService *OrionService) updateOpampConfigFromOrion(config string) (map[string]interface{}, error) {
	body := bytes.NewBuffer([]byte(config))
	req, err := http.NewRequest(
		"PUT", orionService.orionPoller.baseURL+orionService.orionPoller.opampOperatorAPIPath, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("layer-type", "solution")
	req.Header.Set("layer-id", "opamp")
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	var res map[string]interface{}
	if err := orionService.sendRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

// Content-type and body should be already added to req
func (orionService *OrionService) sendRequest(req *http.Request, v interface{}) error {
	req.Header.Set("Accept", "application/json; charset=utf-8")
	req.Header.Set("appd-pid", orionService.orionPoller.principalId)
	req.Header.Set("appd-pty", orionService.orionPoller.principalType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", orionService.orionPoller.apiKey))

	reqDump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Printf("REQUEST:\n%s\n", string(reqDump))

	res, err := orionService.orionPoller.HTTPClient.Do(req)
	if err != nil {
		return err
	}

	respDump, err := httputil.DumpResponse(res, true)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Printf("RESPONSE:\n%s\n", string(respDump))

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

var OrionServ = OrionService{
	orionPoller: &OrionClientInfo{
		apiKey:               "http",
		principalId:          "dXNlcg==",
		principalType:        "c2VydmljZQ==",
		baseURL:              "http://localhost:8084/json/v1",
		opampOperatorAPIPath: "/opamp:operator",
		interval:             defaultCheckInterval,
		HTTPClient: &http.Client{
			Timeout: defaultCheckInterval,
		},
	},
	opampCurrentConfig: &OpampConfiguration{},
}
