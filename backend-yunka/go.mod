module github.com/hvritual/iot-delivery-system/backend-yunka

go 1.25.0

toolchain go1.25.13

require modernc.org/sqlite v1.58.0

require (
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
	yunka.io/framework v0.0.0-00010101000000-000000000000
	yunka.io/gateway v0.0.0-00010101000000-000000000000
)

require (
	github.com/aliyun/aliyun-log-go-sdk v0.1.127 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-sql-driver/mysql v1.7.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/modelcontextprotocol/go-sdk v1.7.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	go.uber.org/atomic v1.10.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260807164820-c8921c73eeea // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0 // indirect
	gorm.io/driver/mysql v1.5.2 // indirect
	gorm.io/gorm v1.25.5 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	yunka.io/pkg v0.0.0-00010101000000-000000000000 // indirect
)

replace yunka.io/framework => ../third_party/yunka/framework

replace yunka.io/pkg => ../third_party/yunka/pkg

replace yunka.io/gateway => ../third_party/yunka/gateway

replace github.com/go-kit/kit => ../third_party/yunka/compat/go-kit-kit-log
