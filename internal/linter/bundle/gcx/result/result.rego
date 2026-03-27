package gcx.result

fail(metadata, details) := violation if {
	violation := _fail_annotated(metadata, details)
}

fail(metadata, details) := violation if {
	violation := _fail_annotated_custom(metadata, details)
}

_fail_annotated(metadata, details) := violation if {
	is_array(metadata) # from rego.metadata.chain()

    rule_meta := metadata[0]
    rule_annotations := metadata[1].annotations

    some resource_type, category, rule
    [_, "rules", resource_type, category, rule, "report"] = rule_meta.path

	violation := {
	    "resource_type": resource_type,
	    "category": category,
	    "rule": rule,
	    "description": rule_annotations.description,
        "severity": rule_level(rule_annotations),
	    "details": details,
	    "related_resources": object.get(rule_annotations, ["related_resources"], [])
	}
}

_fail_annotated_custom(metadata, details) := violation if {
	is_array(metadata) # from rego.metadata.chain()

    rule_meta := metadata[0]
    rule_annotations := metadata[1].annotations

    some resource_type, category, rule
    ["custom", _, "rules", resource_type, category, rule, "report"] = rule_meta.path

	violation := {
	    "resource_type": resource_type,
	    "category": category,
	    "rule": rule,
	    "description": rule_annotations.description,
        "severity": rule_level(rule_annotations),
	    "details": details,
	    "related_resources": object.get(rule_annotations, ["related_resources"], []),
	}
}

default rule_level(_) := "error"

rule_level(annotations) := annotations.custom.severity
