module github.com/gazizov-ai/rsoi-project/statistics

go 1.23.0

require (
	github.com/caarlos0/env/v11 v11.4.0
	github.com/gazizov-ai/rsoi-project/common v0.0.0
	github.com/lib/pq v1.12.3
	github.com/segmentio/kafka-go v0.4.47
)

require (
	github.com/klauspost/compress v1.15.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.15 // indirect
)

replace github.com/gazizov-ai/rsoi-project/common => ../common
