package uisrv

import (
	"context"
	"log"
	"net/http"
	"path"
	"text/template"
	"time"

	"github.com/open-telemetry/opamp-go/internal/examples/server/data"
	"github.com/open-telemetry/opamp-go/internal/examples/server/orionsrv"
	"github.com/open-telemetry/opamp-go/protobufs"
)

var htmlDir string
var srv *http.Server

var logger = log.New(log.Default().Writer(), "[UI] ", log.Default().Flags()|log.Lmsgprefix|log.Lmicroseconds)

func Start(rootDir string) {
	htmlDir = path.Join(rootDir, "uisrv/html")

	mux := http.NewServeMux()
	mux.HandleFunc("/", renderRoot)
	mux.HandleFunc("/agent", renderAgent)
	mux.HandleFunc("/save_config", saveCustomConfigForInstance)
	mux.HandleFunc("/orion", renderOrion)
	mux.HandleFunc("/save_object_config", saveOpampObjectConfiguration)
	srv = &http.Server{
		Addr:    "0.0.0.0:4321",
		Handler: mux,
	}
	go srv.ListenAndServe()
}

func Shutdown() {
	srv.Shutdown(context.Background())
}

func renderTemplate(w http.ResponseWriter, htmlTemplateFile string, data interface{}) {
	t, err := template.ParseFiles(
		path.Join(htmlDir, "header.html"),
		path.Join(htmlDir, htmlTemplateFile),
	)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logger.Printf("Error parsing html template %s: %v", htmlTemplateFile, err)
		return
	}

	err = t.Lookup(htmlTemplateFile).Execute(w, data)
	if err != nil {
		// It is too late to send an HTTP status code since content is already written.
		// We can just log the error.
		logger.Printf("Error writing html content %s: %v", htmlTemplateFile, err)
		return
	}
}

func renderRoot(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "root.html", data.AllAgents.GetAllAgentsReadonlyClone())
}

func renderAgent(w http.ResponseWriter, r *http.Request) {
	agent := data.AllAgents.GetAgentReadonlyClone(data.InstanceId(r.URL.Query().Get("instanceid")))
	if agent == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	renderTemplate(w, "agent.html", agent)
}

func renderOrion(w http.ResponseWriter, r *http.Request) {
	allOpampConfig := orionsrv.OrionServ.GetAllOpampConfiguration()
	if allOpampConfig == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	renderTemplate(w, "orion.html", allOpampConfig)
}

func saveCustomConfigForInstance(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	instanceId := data.InstanceId(r.Form.Get("instanceid"))
	agent := data.AllAgents.GetAgentReadonlyClone(instanceId)
	if agent == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	configStr := r.PostForm.Get("config")
	config := &protobufs.AgentConfigMap{
		ConfigMap: map[string]*protobufs.AgentConfigFile{
			"": {Body: []byte(configStr), ContentType: "application/json"},
		},
	}

	notifyNextStatusUpdate := make(chan struct{}, 1)
	data.AllAgents.SetCustomConfigForAgent(instanceId, config, notifyNextStatusUpdate)

	// Wait for up to 5 seconds for a Status update, which is expected
	// to be reported by the Agent after we set the remote config.
	timer := time.NewTicker(time.Second * 5)

	select {
	case <-notifyNextStatusUpdate:
	case <-timer.C:
	}

	http.Redirect(w, r, "/agent?instanceid="+string(instanceId), http.StatusSeeOther)
}

func saveOpampObjectConfiguration(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	objectType := orionsrv.ObjectType(r.Form.Get("type"))
	if objectType == "" {
		logger.Printf("Object type not found")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	configStr := r.PostForm.Get("config")
	if configStr == "" {
		logger.Printf("Configuration not found")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := orionsrv.OrionServ.UpdateOpampConfiguration(objectType, configStr); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Wait for up to 1 seconds for a Status update, which is expected
	// to be reported by the Agent after we set the remote config.
	timer := time.NewTicker(time.Second * 1)

	select {
	case <-timer.C:
	}

	http.Redirect(w, r, "/orion", http.StatusSeeOther)
}
