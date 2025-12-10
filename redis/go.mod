module github.com/petrijr/fluxo/redis

go 1.25

require (
    github.com/petrijr/fluxo v0.0.0
    github.com/redis/go-redis/v9 v9.17.2
    github.com/testcontainers/testcontainers-go v0.40.0
)

replace github.com/petrijr/fluxo => ..