---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/
title: gcx CLI
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Use gcx to manage Grafana and Grafana Cloud from the terminal and from agentic coding tools.
keywords:
  - gcx
  - Grafana CLI
  - observability as code
weight: 1
cards:
  items:
    - title: Introduction
      description: What `gcx` is, what it costs, and which Grafana versions it works with.
      href: overview/
      height: 24
    - title: Install gcx
      description: Install `gcx` with the quick installer, Homebrew, Windows, a prebuilt binary, or a container.
      href: installation/
      height: 24
    - title: Authenticate gcx
      description: Sign in with browser OAuth, a service account token, or basic authentication.
      href: authentication/
      height: 24
    - title: Configure gcx
      description: Set up contexts with the configuration file or environment variables.
      href: configuration/
      height: 24
    - title: Roles and permissions
      description: Roles and access policy scopes that `gcx` commands need, and how to fix authorization errors.
      href: permissions/
      height: 24
    - title: Work with your resources
      description: Grafana and Grafana Cloud resources you can manage with `gcx`, the commands to use for each, and resource-specific guides.
      href: resources/
      height: 24
    - title: User guides
      description: Search, edit, migrate, and back up resources, diagnose missing telemetry, compare profiles, and check Assistant usage.
      href: guides/
      height: 24
    - title: Usage statistics
      description: What `gcx` reports to Grafana Labs, and how to opt out.
      href: anonymous-usage-statistics/
      height: 24
    - title: Keychain credential storage
      description: How `gcx` stores credentials in your operating system's keychain, and when it falls back to plaintext.
      href: keychain/
      height: 24
    - title: CLI reference
      description: Commands, flags, environment variables, and configuration for the latest stable release.
      href: cli-reference/
      height: 24
    - title: Migrate your configuration
      description: Move a legacy `gcx` configuration to the version 1 format.
      href: migrate-configuration/
      height: 24
  title_class: pt-0 lh-1
hero:
  title: gcx CLI
  description: Manage Grafana and Grafana Cloud from the terminal and from agentic coding tools.
  level: 1
  height: 110
---

{{< docs/hero-simple key="hero" >}}

## Overview

`gcx` is the Grafana CLI that allows you and your AI coding agent to work with Grafana Cloud, Grafana Enterprise, and Grafana OSS.

`gcx` is designed to be used in common scenarios such as authentication against Grafana, query telemetry, inspect and manage resources, and automate observability workflows as code.

It's also integrated with Grafana Assistant, combining the previously fragmented user experience into one single tool.

## Get started

{{< admonition type="note" >}}

Refer to the [`gcx` repository](https://github.com/grafana/gcx) in GitHub for the complete set of `gcx` documents, including architecture information, user guides, and other reference material.

{{< /admonition >}}

{{< card-grid key="cards" type="simple" >}}
