// The e2e suite is a separate module on purpose: it is black-box and imports
// nothing from any service, so it has no reason to depend on their internals.
// A nested module also keeps it out of the root module's ./... patterns.
module github.com/nedo/TicketSaas/tests

go 1.26.4

require github.com/stretchr/testify v1.11.1

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
