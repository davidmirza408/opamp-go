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

	"github.com/open-telemetry/opamp-go/internal/examples/server/data"
	"github.com/open-telemetry/opamp-go/protobufs"
)

var (
	logger = log.New(log.Default().Writer(), "[ORION Poller] ", log.Default().Flags()|log.Lmsgprefix|log.Lmicroseconds)

	defaultCheckInterval = 10 * time.Second

	connectionError = errors.New("OPAMP operator configuration retrieval from ORION failed")
)

type ObjectType string

const (
	// Type of Opamp Solution
	OtelOperator ObjectType = "otelOperator"
	Opsani       ObjectType = "opsani"
)

type OrionService struct {
	orionClient    *OrionClientInfo
	AllOpampConfig *AllOpampConfiguration
}

type OrionClientInfo struct {
	apiKey          string
	principalId     string
	principalType   string
	baseURL         string
	solutionAPIPath string
	interval        time.Duration
	HTTPClient      *http.Client
}

type AllOpampConfiguration struct {
	OtelOperatorConfig *OpampConfiguration
	OpsaniConfig       *OpampConfiguration
}

type OpampConfiguration struct {
	CurrentAllConfig   string
	UpdateObjectConfig string
}

func Start() {
	// go OrionServ.runForEver()
}

func (orionService *OrionService) runForEver() {
	for {
		select {
		case <-time.After(orionService.orionClient.interval):
			orionService.FetchOpampConfiguration()
		}
	}
}

func (orionService *OrionService) GetAllOpampConfiguration() *AllOpampConfiguration {
	orionService.FetchOpampConfiguration()

	return orionService.AllOpampConfig
}

func (orionService *OrionService) UpdateOpampConfiguration(objectType ObjectType, config string) error {
	if _, err := orionService.updateOpampConfigInOrion(objectType, config); err != nil {
		return err
	}

	return nil
}

func (orionService *OrionService) FetchOpampConfiguration() {
	orionService.fetchOpampConfigFromOrion(OtelOperator)
	orionService.fetchOpampConfigFromOrion(Opsani)
}

func (orionService *OrionService) fetchOpampConfigFromOrion(objectType ObjectType) {
	logger.Printf("Checking in Orion updated config for ", objectType)

	res, err := orionService.getOpampConfigFromOrion(objectType)
	if err != nil {
		logger.Printf("Orion config retrieval failed: ", err)
		return
	}

	var opampConfig *OpampConfiguration
	switch objectType {
	case OtelOperator:
		opampConfig = orionService.AllOpampConfig.OtelOperatorConfig
	case Opsani:
		opampConfig = orionService.AllOpampConfig.OpsaniConfig
	default:
		panic("Invalid OPAMP object type")
	}

	resIndented, err := json.MarshalIndent(res, "", "    ")
	opampConfig.CurrentAllConfig = string(resIndented)
	opampConfig.UpdateObjectConfig = ""

	itemsSlice := res["items"].([]interface{})
	if len(itemsSlice) > 0 {
		for _, item := range itemsSlice {
			objConfig := item.(map[string]interface{})["object"]

			operationInfo := objConfig.(map[string]interface{})["operationInfo"]
			objName := operationInfo.(map[string]interface{})["name"].(string)
			// logger.Printf("Retrieved OPAMP config: ", itemConfigVal)

			objConfigJsonBytes, err := json.Marshal(objConfig.(map[string]interface{}))
			if err != nil {
				logger.Printf("OPAMP config JSON parse error msg: ", err)
				return
			}

			saveCustomConfigForAllInstance(objName, objConfigJsonBytes)
		}
	}
}

func (orionService *OrionService) getOpampConfigFromOrion(objectType ObjectType) (map[string]interface{}, error) {
	url := orionService.orionClient.baseURL + orionService.orionClient.solutionAPIPath + string(objectType)
	req, err := http.NewRequest("GET", url, nil)
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

func (orionService *OrionService) updateOpampConfigInOrion(objectType ObjectType, config string) (map[string]interface{}, error) {
	url := orionService.orionClient.baseURL + orionService.orionClient.solutionAPIPath + string(objectType)
	body := bytes.NewBuffer([]byte(config))
	req, err := http.NewRequest("PUT", url, body)
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
	req.Header.Set("appd-pid", orionService.orionClient.principalId)
	req.Header.Set("appd-pty", orionService.orionClient.principalType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", orionService.orionClient.apiKey))

	reqDump, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Printf("REQUEST:\n%s\n", string(reqDump))

	res, err := orionService.orionClient.HTTPClient.Do(req)
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

func saveCustomConfigForAllInstance(objName string, configBytes []byte) {
	config := &protobufs.AgentConfigMap{
		ConfigMap: map[string]*protobufs.AgentConfigFile{
			objName: {Body: configBytes, ContentType: "application/json"},
		},
	}

	data.AllAgents.SetCustomConfigForAllAgent(config)
}

var OrionServ = OrionService{
	orionClient: &OrionClientInfo{
		apiKey:          "http",
		principalId:     "dXNlcg==",
		principalType:   "c2VydmljZQ==",
		baseURL:         "http://localhost:8084/json/v1",
		solutionAPIPath: "/opamp:",
		interval:        defaultCheckInterval,
		HTTPClient: &http.Client{
			Timeout: defaultCheckInterval,
		},
	},
	AllOpampConfig: &AllOpampConfiguration{
		OtelOperatorConfig: &OpampConfiguration{},
		OpsaniConfig:       &OpampConfiguration{},
	},
}
