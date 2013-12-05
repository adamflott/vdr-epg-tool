all:
	go build vdr-epg-tool.go

format:
	gofmt -tabs=false -tabwidth=4 -w=true vdr-epg-tool.go
