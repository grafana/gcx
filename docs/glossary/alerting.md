# Alerting

Terms for Grafana Alerting notification routing.

**Routing tree**:
A hierarchy of notification policies that decides which receiver gets an alert and with what grouping and timing. A stack has one default tree and may have named trees.
_Avoid_: policy tree (for named trees), route set

**Default tree**:
The routing tree every stack has, named `user-defined`. Deleting it resets it to Grafana's built-in configuration instead of removing it.
_Avoid_: root policy, main tree

**Named tree**:
An additional routing tree with its own name, created and deleted independently of the default tree.
_Avoid_: custom policy, secondary tree

**Notification policy**:
One node in a routing tree: matchers that select alerts, plus the receiver and timing applied to them. Child policies refine their parent.
_Avoid_: route (in user-facing text), rule

**Receiver**:
A named notification destination that a policy points to. It holds one or more integrations.
_Avoid_: contact point (the Grafana UI name for the same thing; use it only when quoting the UI or the `contact-points` commands)

**Integration**:
A single delivery configuration inside a receiver, such as one Slack channel or one email address, with its own settings and possibly credentials.
_Avoid_: notifier, channel

**Template group**:
A named set of notification templates that receivers use to format messages.
_Avoid_: template file

**Time interval**:
A named set of time ranges that policies use to mute or allow notifications.
_Avoid_: mute timing (the older provisioning name; use it only when referring to the `mute-timings` commands)
