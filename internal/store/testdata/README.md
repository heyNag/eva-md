# The store's test data

`fixture-store` is a v2 store written by hand for the store's tests. Every value in it is made up, its ids too.

A project's id is `p_` and the first 60 bits of SHA-256 of the first key that named it (`docs/spec/store-format.md`, section 3; `ProjectID` in `internal/ledger/registry`). The fixture was written before that function, and one of its project ids does not follow the rule: it hashes the remote `github.com/acme/orders`, where its first legacy key `github.com__acme__orders` gives another id. The fixture is not made again, since a new id would rename the project's folder and its two branch workstreams, whose ids hash the project id.

The project ids of the fixture that do not follow the rule, one a line:

- `p_00gb3367a19r`

Every other project id of the fixture is `ProjectID` of its first legacy key. A test of `internal/ledger/registry` holds this list to the fixture: an id listed here that follows the rule, or that no project record holds, fails it.
