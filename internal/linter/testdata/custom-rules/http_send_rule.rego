package custom.pkg.rules.dashboard.security.http_send_rule

# METADATA
# title: Rule using http.send (disallowed)
# description: This rule uses http.send and should fail to compile.

deny contains msg if {
	response := http.send({"method": "GET", "url": "http://example.com"})
	response.status_code != 200
	msg := "external check failed"
}
