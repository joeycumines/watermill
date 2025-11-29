module test2

replace github.com/ThreeDotsLabs/watermill => ../..

require (
	github.com/ThreeDotsLabs/watermill v0.0.0-00010101000000-000000000000
	github.com/joeycumines/go-bigbuff v1.21.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/lithammer/shortuuid/v3 v3.0.7 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
)

go 1.25
