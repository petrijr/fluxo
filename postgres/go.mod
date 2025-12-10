module github.com/petrijr/fluxo/postgres

go 1.25

require (
	github.com/petrijr/fluxo v0.0.0
	github.com/jackc/pgx/v5 v5.5.0
	github.com/testcontainers/testcontainers-go v0.40.0
)

replace github.com/petrijr/fluxo => ..