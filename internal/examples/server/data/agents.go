package data

import (
	"log"
	"sync"

	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server/types"
)

var logger = log.New(log.Default().Writer(), "[OPAMP] ", log.Default().Flags()|log.Lmsgprefix|log.Lmicroseconds)

type Agents struct {
	mux         sync.RWMutex
	agentsById  map[InstanceId]*Agent
	connections map[types.Connection]map[InstanceId]bool
}

// RemoveConnection removes the connection all Agent instances associated with the
// connection.
func (agents *Agents) RemoveConnection(conn types.Connection) {
	agents.mux.Lock()
	defer agents.mux.Unlock()

	for instanceId := range agents.connections[conn] {
		logger.Printf("Removed connection from agent: ", string(instanceId))
		delete(agents.agentsById, instanceId)
	}
	delete(agents.connections, conn)
}

func (agents *Agents) SetCustomConfigForAgent(
	agentId InstanceId,
	config *protobufs.AgentConfigMap,
	notifyNextStatusUpdate chan<- struct{},
) {
	agent := agents.FindAgent(agentId)
	if agent != nil {
		agent.SetCustomConfig(config, notifyNextStatusUpdate)
	}
}

func (agents *Agents) SetCustomConfigForAllAgent(config *protobufs.AgentConfigMap) {
	for _, agent := range agents.agentsById {
		agent.SetCustomConfig(config, nil)
	}
}

func (agents *Agents) FindAgent(agentId InstanceId) *Agent {
	agents.mux.RLock()
	defer agents.mux.RUnlock()
	return agents.agentsById[agentId]
}

func (agents *Agents) FindOrCreateAgent(agentId InstanceId, conn types.Connection) *Agent {
	agents.mux.Lock()
	defer agents.mux.Unlock()

	// Ensure the Agent is in the agentsById map.
	agent := agents.agentsById[agentId]
	if agent == nil {
		agent = NewAgent(agentId, conn)
		agents.agentsById[agentId] = agent

		// Ensure the Agent's instance id is associated with the connection.
		if agents.connections[conn] == nil {
			agents.connections[conn] = map[InstanceId]bool{}
		}
		agents.connections[conn][agentId] = true
	}

	return agent
}

func (agents *Agents) GetAgentReadonlyClone(agentId InstanceId) *Agent {
	agent := agents.FindAgent(agentId)
	if agent == nil {
		return nil
	}

	// Return a clone to allow safe access after returning.
	return agent.CloneReadonly()
}

func (agents *Agents) GetAllAgentsReadonlyClone() map[InstanceId]*Agent {
	agents.mux.RLock()

	// Clone the map first
	m := map[InstanceId]*Agent{}
	for id, agent := range agents.agentsById {
		m[id] = agent
	}
	agents.mux.RUnlock()

	// Clone agents in the map
	for id, agent := range m {
		// Return a clone to allow safe access after returning.
		m[id] = agent.CloneReadonly()
	}
	return m
}

var AllAgents = Agents{
	agentsById:  map[InstanceId]*Agent{},
	connections: map[types.Connection]map[InstanceId]bool{},
}
