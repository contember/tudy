module github.com/contember/tudy/cmd/cli

go 1.24.0

require (
	github.com/contember/tudy/cmd/shared v0.0.0
	github.com/contember/tudy/llm_resolver v0.0.0
)

replace (
	github.com/contember/tudy/cmd/shared => ../shared
	github.com/contember/tudy/llm_resolver => ../../llm_resolver
)
