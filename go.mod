module github.com/ThreeDotsLabs/watermill

go 1.24.0

toolchain go1.24.3

require (
	github.com/cenkalti/backoff/v5 v5.0.3
	github.com/go-chi/chi/v5 v5.2.3
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.4
	github.com/google/uuid v1.6.0
	github.com/lithammer/shortuuid/v3 v3.0.7
	github.com/oklog/ulid v1.3.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.23.2
	github.com/sony/gobreaker v1.0.0
	github.com/stretchr/testify v1.11.1
	google.golang.org/protobuf v1.36.10
)

require (
	github.com/BurntSushi/toml v1.5.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.4 // indirect
	github.com/prometheus/procfs v0.19.2 // indirect
	github.com/yuin/goldmark v1.7.13 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/exp/typeparams v0.0.0-20251125195548-87e1e737ad39 // indirect
	golang.org/x/mod v0.30.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/tools v0.39.0 // indirect
	golang.org/x/tools/cmd/godoc v0.1.0-deprecated // indirect
	golang.org/x/tools/go/expect v0.1.1-deprecated // indirect
	golang.org/x/tools/godoc v0.1.0-deprecated // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	honnef.co/go/tools v0.6.1 // indirect
)

tool (
	golang.org/x/tools/cmd/godoc
	honnef.co/go/tools/cmd/staticcheck
)
