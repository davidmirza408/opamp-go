package opampsrv

import (
	"context"
	"log"

	"github.com/golang/protobuf/proto"
	clientTypes "github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/internal/examples/server/data"
	"github.com/open-telemetry/opamp-go/internal/examples/server/orionsrv"
	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"
)

type Server struct {
	logger   clientTypes.Logger
	opampSrv server.OpAMPServer
	agents   *data.Agents
}

func NewServer(agents *data.Agents) *Server {
	logger := log.New(
		log.Default().Writer(),
		"[Server] ",
		log.Default().Flags()|log.Lmsgprefix|log.Lmicroseconds,
	)

	srv := &Server{
		logger: &Logger{logger: logger},
		agents: agents,
	}

	srv.opampSrv = server.New(srv.logger)

	return srv
}

func (srv *Server) Start() {
	settings := server.StartSettings{
		Settings: server.Settings{
			Callbacks: server.CallbacksStruct{
				OnMessageFunc:         srv.onMessage,
				OnConnectionCloseFunc: srv.onDisconnect,
			},
		},
		ListenEndpoint: "127.0.0.1:4320",
	}

	err := srv.opampSrv.Start(settings)
	if err != nil {
		panic(err)
	}
}

func (srv *Server) Stop() {
	srv.opampSrv.Stop(context.Background())
}

func (srv *Server) onDisconnect(conn types.Connection) {
	srv.agents.RemoveConnection(conn)
}

func (srv *Server) onMessage(conn types.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	srv.logger.Debugf("Received message: ", proto.MarshalTextString(msg))
	instanceId := data.InstanceId(msg.InstanceUid)

	agent := srv.agents.FindOrCreateAgent(instanceId, conn)

	// Start building the response.
	response := &protobufs.ServerToAgent{}

	// Process the status report and continue building the response.
	agent.UpdateStatus(msg, response)

	orionsrv.OrionServ.FetchAndUpdateLocalRemoteConfigs()

	// Send the response back to the Agent.
	srv.logger.Debugf("Sending message: ", proto.MarshalTextString(response))
	return response
}
