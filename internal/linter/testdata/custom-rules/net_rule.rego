package custom.pkg.rules.dashboard.security.net_rule

# METADATA
# title: Rule using net.lookup_ip_addr (disallowed)
# description: This rule uses a net.* builtin and should fail to compile.

deny contains msg if {
	addrs := net.lookup_ip_addr("example.com")
	count(addrs) > 0
	msg := "resolved external host"
}
