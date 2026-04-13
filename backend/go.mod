module hsgram-admin/backend

go 1.23.0

replace github.com/teamgram/teamgram-server => ../../HSgram_server

require (
	github.com/go-sql-driver/mysql v1.9.3
	github.com/golang-jwt/jwt/v4 v4.5.2
	github.com/teamgram/proto v0.223.1
	github.com/teamgram/teamgram-server v0.0.0-00010101000000-000000000000
	golang.org/x/crypto v0.41.0
	google.golang.org/grpc v1.65.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
