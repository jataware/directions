module github.com/jataware/directions/clients/cli

go 1.24.0

require github.com/jataware/directions/clients/lib/go v0.0.0

require github.com/pelletier/go-toml/v2 v2.2.3 // indirect

replace github.com/jataware/directions/clients/lib/go => ../lib/go
