module github.com/containerd/go-cni

require (
	github.com/containernetworking/cni v1.1.2
	github.com/stretchr/testify v1.6.1
)

go 1.13

replace github.com/containernetworking/cni v1.1.2 => github.com/kevpar/cni v0.0.0-20231128095630-ced4b2e2c923
