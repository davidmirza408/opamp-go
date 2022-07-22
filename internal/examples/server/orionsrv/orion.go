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

type LayerType string

const (
	// Type of Opamp Layer
	Solution LayerType = "solution"
	Tenant   LayerType = "tenant"
)

type OrionService struct {
	orionClient    *OrionClientInfo
	AllOpampConfig *AllOpampConfiguration
}

type OrionClientInfo struct {
	apiKey          string
	baseURL         string
	solutionAPIPath string
	interval        time.Duration
	HTTPClient      *http.Client
}

type AllOpampConfiguration struct {
	DeploymentConfig     *OpampConfiguration
	OtelOperatorConfig   *OpampConfiguration
	OtelStandaloneConfig *OpampConfiguration
}

type OpampConfiguration struct {
	AllSolutionConfig  string
	AllTenantConfig    string
	UpdateObjectConfig string
	PatchObjectConfig  string
}

func Start() {
	// go OrionServ.runForEver()
}

func (orionService *OrionService) runForEver() {
	for {
		select {
		case <-time.After(orionService.orionClient.interval):
			orionService.FetchAndUpdateLocalRemoteConfigs()
		}
	}
}

func (orionService *OrionService) GetOpampConfiguration(objectType data.ObjectType) *OpampConfiguration {
	logger.Printf("Refreshing Orion Configuration UI....")
	orionService.FetchAndUpdateLocalRemoteConfigs()

	var opampConfig *OpampConfiguration
	switch objectType {
	case data.Deployment:
		opampConfig = orionService.AllOpampConfig.DeploymentConfig
	case data.OtelOperator:
		opampConfig = orionService.AllOpampConfig.OtelOperatorConfig
	case data.OtelStandalone:
		opampConfig = orionService.AllOpampConfig.OtelStandaloneConfig
	default:
		panic("Invalid OPAMP object type")
	}

	return opampConfig
}

func (orionService *OrionService) UpsertOpampConfiguration(objectType data.ObjectType, layerType LayerType, config string) error {
	if _, err := orionService.upsertOpampConfigInOrion(objectType, layerType, config); err != nil {
		return err
	}
	orionService.FetchAndUpdateLocalRemoteConfigs()

	data.AllAgents.PushRemoteConfigForAllAgents()
	return nil
}

func (orionService *OrionService) PatchOpampConfiguration(objectType data.ObjectType, objectId string, config string) error {
	logger.Printf("Patching in Orion config for type %s and id %s", objectType, objectId)
	if err := orionService.patchOpampConfigInOrion(objectType, objectId, config); err != nil {
		return err
	}
	orionService.FetchAndUpdateLocalRemoteConfigs()

	data.AllAgents.PushRemoteConfigForAllAgents()
	return nil
}

func (orionService *OrionService) FetchAndUpdateLocalRemoteConfigs() {
	orionService.fetchAndUpdateLocalRemoteConfigFromOrion(data.Deployment, Solution)
	orionService.fetchAndUpdateLocalRemoteConfigFromOrion(data.OtelOperator, Solution)
	orionService.fetchAndUpdateLocalRemoteConfigFromOrion(data.OtelStandalone, Solution)

	orionService.fetchAndUpdateLocalRemoteConfigFromOrion(data.Deployment, Tenant)
	orionService.fetchAndUpdateLocalRemoteConfigFromOrion(data.OtelOperator, Tenant)
	orionService.fetchAndUpdateLocalRemoteConfigFromOrion(data.OtelStandalone, Tenant)
}

func (orionService *OrionService) fetchAndUpdateLocalRemoteConfigFromOrion(objectType data.ObjectType, layerType LayerType) {
	logger.Printf("Checking in Orion updated config for ", objectType)

	res, err := orionService.getOpampConfigFromOrion(objectType, layerType)
	if err != nil {
		logger.Printf("Orion config retrieval failed: ", err)
		return
	}

	var opampConfig *OpampConfiguration
	switch objectType {
	case data.Deployment:
		opampConfig = orionService.AllOpampConfig.DeploymentConfig
	case data.OtelOperator:
		opampConfig = orionService.AllOpampConfig.OtelOperatorConfig
	case data.OtelStandalone:
		opampConfig = orionService.AllOpampConfig.OtelStandaloneConfig
	default:
		panic("Invalid OPAMP object type")
	}

	resIndented, err := json.MarshalIndent(res, "", "    ")

	if layerType == Tenant {
		opampConfig.AllTenantConfig = string(resIndented)
		orionService.updateLocalRemoteConfig(objectType, res)
	} else {
		opampConfig.AllSolutionConfig = string(resIndented)
	}
	opampConfig.UpdateObjectConfig = ""
	opampConfig.PatchObjectConfig = ""
}

func (orionService *OrionService) updateLocalRemoteConfig(objectType data.ObjectType, opampConfig map[string]interface{}) {
	itemsSlice := opampConfig["items"].([]interface{})
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

			orionService.setLocalRemoteConfigForAllAgents(objectType, objName, objConfigJsonBytes)
		}
	}
}

func (orionService *OrionService) getOpampConfigFromOrion(objectType data.ObjectType, layerType LayerType) (map[string]interface{}, error) {
	url := orionService.orionClient.baseURL + orionService.orionClient.solutionAPIPath + string(objectType)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	orionService.setHeaders(req, layerType)

	var res map[string]interface{}
	if err := orionService.sendRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (orionService *OrionService) upsertOpampConfigInOrion(objectType data.ObjectType, layerType LayerType, config string) (map[string]interface{}, error) {
	url := orionService.orionClient.baseURL + orionService.orionClient.solutionAPIPath + string(objectType)
	body := bytes.NewBuffer([]byte(config))
	req, err := http.NewRequest("PUT", url, body)
	if err != nil {
		return nil, err
	}

	orionService.setHeaders(req, layerType)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	var res map[string]interface{}
	if err := orionService.sendRequest(req, &res); err != nil {
		return nil, err
	}

	return res, nil
}

func (orionService *OrionService) patchOpampConfigInOrion(objectType data.ObjectType, objectId string, config string) error {
	url := orionService.orionClient.baseURL + orionService.orionClient.solutionAPIPath + string(objectType) + "/" + objectId
	body := bytes.NewBuffer([]byte(config))
	req, err := http.NewRequest("PATCH", url, body)
	if err != nil {
		logger.Printf("HTTP request error %v", err)
		return err
	}

	orionService.setHeaders(req, Tenant)
	req.Header.Set("Content-Type", "application/json-patch+json")

	if err := orionService.sendRequest(req, nil); err != nil {
		logger.Printf("Request send error %v", err)
		return err
	}

	return nil
}

// Content-type and body should be already added to req
func (orionService *OrionService) sendRequest(req *http.Request, v interface{}) error {
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

	if v != nil {
		if err = json.NewDecoder(res.Body).Decode(&v); err != nil {
			return err
		}
		// logger.Printf("Retrieved OPAMP object: ", v)
	}

	return nil
}

func (orionService *OrionService) setHeaders(req *http.Request, layerType LayerType) {
	req.Header.Set("Accept", "application/json; charset=utf-8")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", orionService.orionClient.apiKey))

	if layerType == Solution {
		req.Header.Set("appd-pty", "U0VSVklDRQ==")
		req.Header.Set("appd-pid", "VVNFUg==")
		req.Header.Set("layer-type", "solution")
		req.Header.Set("layer-id", "opamp")
	} else {
		req.Header.Set("appd-pty", "VVNFUg==")
		req.Header.Set("appd-pid", "dGVuYW50MQ==")
		req.Header.Set("layer-type", "tenant")
		req.Header.Set("layer-id", "tenant1")
		req.Header.Set("appd-tid", "dGVuYW50MQ==")
	}
}

func (orionService *OrionService) setLocalRemoteConfigForAllAgents(objectType data.ObjectType, objName string, configBytes []byte) {
	config := &protobufs.AgentConfigMap{
		ConfigMap: map[string]*protobufs.AgentConfigFile{
			objName: {Body: configBytes, ContentType: "application/json"},
		},
	}

	data.AllAgents.SetLocalRemoteConfigForAllAgents(objectType, config)
}

var OrionServ = OrionService{
	orionClient: &OrionClientInfo{
		apiKey:          "http",
		baseURL:         "http://localhost:8084/json/v1beta/objects",
		solutionAPIPath: "/opamp:",
		interval:        defaultCheckInterval,
		HTTPClient: &http.Client{
			Timeout: defaultCheckInterval,
		},
	},
	AllOpampConfig: &AllOpampConfiguration{
		DeploymentConfig:     &OpampConfiguration{},
		OtelOperatorConfig:   &OpampConfiguration{},
		OtelStandaloneConfig: &OpampConfiguration{},
	},
}
