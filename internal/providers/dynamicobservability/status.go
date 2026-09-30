package dynamicobservability

import (
	"sort"
	"time"
)

type probe struct {
	Metadata struct {
		Name              string `json:"name"`
		Generation        int64  `json:"generation"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Title  string `json:"title"`
		Paused bool   `json:"paused"`
		Target struct {
			Cluster   string `json:"cluster"`
			Namespace string `json:"namespace"`
		} `json:"target"`
	} `json:"spec"`
	Status struct {
		Nodes []probeNode `json:"nodes"`
	} `json:"status"`
}

type probeNode struct {
	NodeName           string        `json:"nodeName"`
	ObservedGeneration int64         `json:"observedGeneration"`
	CheckedAt          string        `json:"checkedAt"`
	Phase              string        `json:"phase"`
	Reason             string        `json:"reason,omitempty"`
	Message            string        `json:"message,omitempty"`
	Targets            []probeTarget `json:"targets,omitempty"`
}

type probeTarget struct {
	RuleName      string `json:"ruleName,omitempty"`
	PodName       string `json:"podName"`
	ContainerName string `json:"containerName"`
	PID           int64  `json:"pid"`
	Phase         string `json:"phase"`
	Reason        string `json:"reason,omitempty"`
	Message       string `json:"message,omitempty"`
}

type nodeStatus struct {
	probeNode

	EffectivePhase string `json:"effectivePhase"`
}

type rulesetStatus struct {
	Name            string       `json:"name"`
	Title           string       `json:"title"`
	Cluster         string       `json:"cluster"`
	Namespace       string       `json:"namespace"`
	Paused          bool         `json:"paused"`
	Generation      int64        `json:"generation"`
	Phase           string       `json:"phase"`
	AttachedTargets int          `json:"attachedTargets"`
	ErrorTargets    int          `json:"errorTargets"`
	Nodes           []nodeStatus `json:"nodes"`
}

func probeStatus(p probe, now time.Time) rulesetStatus {
	result := rulesetStatus{
		Name: p.Metadata.Name, Title: p.Spec.Title, Cluster: p.Spec.Target.Cluster,
		Namespace: p.Spec.Target.Namespace, Paused: p.Spec.Paused,
		Generation: p.Metadata.Generation, Nodes: make([]nodeStatus, 0, len(p.Status.Nodes)),
	}
	if len(p.Status.Nodes) == 0 {
		result.Phase = "Unknown"
		if recent(p.Metadata.CreationTimestamp, now, 150*time.Second) {
			result.Phase = "Pending"
		}
		return result
	}
	var attached, pending, unknown, failed, unsupported, paused int
	for _, node := range p.Status.Nodes {
		phase := node.Phase
		if !recent(node.CheckedAt, now, 150*time.Second) {
			phase = "Unknown"
		} else if node.ObservedGeneration != p.Metadata.Generation || (p.Spec.Paused && phase == "Attached") || (!p.Spec.Paused && phase == "Paused") {
			phase = "Pending"
		}
		result.Nodes = append(result.Nodes, nodeStatus{probeNode: node, EffectivePhase: phase})
		switch phase {
		case "Attached":
			attached++
		case "Paused":
			paused++
		case "Pending":
			pending++
		case "Unknown":
			unknown++
		case "Unsupported":
			unsupported++
		case "Error":
			failed++
		}
		if phase != "Pending" && phase != "Unknown" {
			for _, target := range node.Targets {
				switch target.Phase {
				case "Attached":
					result.AttachedTargets++
				case "Error":
					result.ErrorTargets++
				}
			}
		}
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].NodeName < result.Nodes[j].NodeName })
	result.Phase = aggregateProbePhase(p.Spec.Paused, p.Metadata.Generation, result.Nodes, attached, pending, unknown, failed, unsupported, paused)
	return result
}

func aggregateProbePhase(isPaused bool, generation int64, nodes []nodeStatus, attached, pending, unknown, failed, unsupported, paused int) string {
	blockingPending := false
	for _, node := range nodes {
		if node.EffectivePhase == "Pending" && (attached == 0 || node.Reason != "NoTarget" || node.ObservedGeneration != generation) {
			blockingPending = true
			break
		}
	}
	switch {
	case failed > 0:
		return "Error"
	case isPaused:
		switch {
		case pending > 0:
			return "Pending"
		case unknown > 0:
			return "Unknown"
		case paused == len(nodes):
			return "Paused"
		default:
			return "Error"
		}
	case blockingPending:
		return "Pending"
	case attached > 0 && unsupported > 0:
		return "Error"
	case attached > 0:
		return "Attached"
	case unknown > 0:
		return "Unknown"
	case unsupported == len(nodes):
		return "Unsupported"
	default:
		return "Pending"
	}
}

type nodeAgent struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Cluster              string `json:"cluster"`
		NodeName             string `json:"nodeName"`
		Version              string `json:"version"`
		LastSeen             string `json:"lastSeen"`
		Ready                bool   `json:"ready"`
		RuntimeReady         bool   `json:"runtimeReady"`
		CacheSynced          bool   `json:"cacheSynced"`
		SourceWatchConnected bool   `json:"sourceWatchConnected"`
		DesiredProbes        int    `json:"desiredProbes"`
		ActiveAttachments    int    `json:"activeAttachments"`
		DiscoveredProcesses  int    `json:"discoveredProcesses"`
		ObservedHits         int    `json:"observedHits"`
	} `json:"spec"`
}

type agentStatus struct {
	Name                 string `json:"name"`
	Cluster              string `json:"cluster"`
	NodeName             string `json:"nodeName"`
	Connection           string `json:"connection"`
	Health               string `json:"health"`
	LastSeen             string `json:"lastSeen"`
	Version              string `json:"version"`
	Ready                bool   `json:"ready"`
	RuntimeReady         bool   `json:"runtimeReady"`
	CacheSynced          bool   `json:"cacheSynced"`
	SourceWatchConnected bool   `json:"sourceWatchConnected"`
	DesiredProbes        int    `json:"desiredProbes"`
	ActiveAttachments    int    `json:"activeAttachments"`
	DiscoveredProcesses  int    `json:"discoveredProcesses"`
	ObservedHits         int    `json:"observedHits"`
}

func agentHealth(a nodeAgent, now time.Time) agentStatus {
	r := agentStatus{
		Name: a.Metadata.Name, Cluster: a.Spec.Cluster, NodeName: a.Spec.NodeName,
		LastSeen: a.Spec.LastSeen, Version: a.Spec.Version, Ready: a.Spec.Ready,
		RuntimeReady: a.Spec.RuntimeReady, CacheSynced: a.Spec.CacheSynced,
		SourceWatchConnected: a.Spec.SourceWatchConnected, DesiredProbes: a.Spec.DesiredProbes,
		ActiveAttachments: a.Spec.ActiveAttachments, DiscoveredProcesses: a.Spec.DiscoveredProcesses,
		ObservedHits: a.Spec.ObservedHits,
	}
	switch {
	case !validTime(a.Spec.LastSeen, now):
		r.Connection = "Unknown"
	case !recent(a.Spec.LastSeen, now, 60*time.Second):
		r.Connection = "Disconnected"
	default:
		r.Connection = "Connected"
	}
	switch {
	case r.Connection != "Connected":
		r.Health = r.Connection
	case r.Ready && r.RuntimeReady && r.CacheSynced && r.SourceWatchConnected:
		r.Health = "Ready"
	default:
		r.Health = "Degraded"
	}
	return r
}

func validTime(value string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !t.After(now.Add(30*time.Second))
}

func recent(value string, now time.Time, maxAge time.Duration) bool {
	t, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !t.After(now.Add(30*time.Second)) && now.Sub(t) <= maxAge
}
