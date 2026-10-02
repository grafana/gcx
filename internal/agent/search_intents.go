package agent

// CommandSearchTerms adds positive task vocabulary that the terse command tree
// cannot express. These entries describe existing capabilities, not query-specific
// boosts. Keep workflow instructions in the bundled skills instead.
func CommandSearchTerms() map[string]string {
	return map[string]string{
		"gcx dashboards list":                           "dashboard inventory browse list dashboards",
		"gcx dashboards search":                         "find locate lookup dashboard title name tag folder",
		"gcx dashboards create":                         "create add upload new dashboard JSON manifest file",
		"gcx dashboards snapshot":                       "dashboard screenshot image PNG capture render download",
		"gcx dashboards list-versions":                  "dashboard history previous revision version",
		"gcx dashboards versions restore":               "dashboard rollback revert undo restore previous version changes",
		"gcx resources pull":                            "export backup download save local dashboard folder resource manifest YAML JSON files disk",
		"gcx resources push":                            "import upload apply publish dashboard folder resource manifest YAML JSON files",
		"gcx resources validate":                        "validate verify check valid validity dashboard resource manifest JSON YAML local upload",
		"gcx resources list-types":                      "resource dashboard folder kinds types schema OpenAPI fields supported available",
		"gcx datasources list":                          "datasource inventory connections configured list",
		"gcx datasources health":                        "datasource health healthy connectivity connections working test verify check",
		"gcx datasources schemas get":                   "datasource plugin configuration schema fields type configure",
		"gcx metrics query":                             "query run execute fetch retrieve metrics values PromQL Prometheus",
		"gcx logs query":                                "query run execute fetch retrieve search application logs errors LogQL Loki",
		"gcx traces query":                              "query search find distributed traces service TraceQL",
		"gcx profiles query":                            "query fetch retrieve inspect CPU continuous profiling profiles samples",
		"gcx config current-context":                    "context current active selected using name",
		"gcx config use-context":                        "context switch change select different another stack active",
		"gcx config check":                              "configuration context issues problems diagnose validate check",
		"gcx cloud login":                               "cloud platform login authentication authenticate sign in setup",
		"gcx cloud stacks list":                         "cloud stack organisation inventory list",
		"gcx agent skills list":                         "agent skill guide bundled installed available browse inventory list",
		"gcx agent skills install":                      "agent skill guide bundled install add setup local",
		"gcx synthetic-monitoring checks create":        "create add setup monitor website HTTP uptime availability minute synthetic check",
		"gcx synthetic-monitoring checks status":        "synthetic check status health failing failures report",
		"gcx synthetic-monitoring probes list":          "synthetic monitoring check probe locations where run available list",
		"gcx slo definitions status":                    "SLO compliance status remaining left error budget check",
		"gcx irm incidents create":                      "incident create declare new outage",
		"gcx irm incidents close":                       "incident close closed resolve finish",
		"gcx irm oncall schedules list-final-shifts":    "oncall schedule final shifts rota who now current",
		"gcx irm oncall integrations start-maintenance": "oncall integration maintenance window suppress pause escalation temporarily",
		"gcx alert contact-points list":                 "alert notification destinations contact points configured list",
		"gcx fleet collectors list":                     "fleet collector registered inventory list",
		"gcx k6 load-tests list":                        "k6 performance load test list",
		"gcx fleet pipelines update":                    "fleet pipeline configuration change modify edit update",
		"gcx assistant investigations create":           "assistant AI investigation investigate ask start launch create",
	}
}

// SearchWorkflow is a curated route to an existing, read-only skill guide.
type SearchWorkflow struct {
	Skill       string
	Description string
	Terms       string
}

// SearchWorkflows deliberately indexes positive routing terms only: full skill
// documents also contain exclusions and examples of unrelated operations.
func SearchWorkflows() []SearchWorkflow {
	return []SearchWorkflow{
		{"debug-with-grafana", "Open a guide to investigating application problems using Grafana signals", "investigate diagnose debug application problem high CPU memory usage latency slow request errors regression root cause"},
		{"manage-dashboards", "Open a guide to managing and transferring dashboards between Grafana instances", "move migrate transfer promote dashboard another different Grafana instance environment"},
		{"investigate-alert", "Open a guide to investigating why a Grafana alert rule is firing", "investigate diagnose why alert rule firing evaluation"},
		{"oncall-triage", "Open a guide to triaging active OnCall alert groups", "triage oncall active paging alert groups acknowledge silence resolve"},
		{"slo-investigate", "Open a guide to investigating a breaching SLO", "investigate diagnose why SLO breaching burning error budget root cause"},
		{"synth-investigate-check", "Open a guide to investigating a failing synthetic check", "investigate diagnose why synthetic monitoring check failing failure"},
	}
}
