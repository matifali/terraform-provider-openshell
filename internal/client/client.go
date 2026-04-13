// SPDX-License-Identifier: MPL-2.0

// Package client wraps the OpenShell gateway gRPC API into a
// high-level Go client that the Terraform provider resources and
// data sources consume.
package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	pb "github.com/nvidia/terraform-provider-openshell/proto/openshellv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Client holds the gRPC connection and service stubs.
type Client struct {
	conn      *grpc.ClientConn
	OpenShell pb.OpenShellClient
	Inference pb.InferenceClient
}

// Config describes how to connect to an OpenShell gateway.
type Config struct {
	// GatewayURL is the gRPC endpoint, e.g. "localhost:8443".
	GatewayURL string

	// mTLS paths — all three are required for mTLS auth.
	CACert string
	Cert   string
	Key    string

	// Token is a bearer token for edge/remote gateways.  When set
	// mTLS fields are ignored.
	Token string

	// Insecure disables TLS entirely (useful for local dev).
	Insecure bool
}

// New dials the gateway and returns a ready-to-use Client.
func New(ctx context.Context, cfg Config) (*Client, error) {
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(64 << 20)),
	}

	switch {
	case cfg.Insecure:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	case cfg.Token != "":
		// Bearer-token auth over TLS (no client certs).
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if cfg.CACert != "" {
			pool, err := certPool(cfg.CACert)
			if err != nil {
				return nil, err
			}
			tlsCfg.RootCAs = pool
		}
		opts = append(opts,
			grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
			grpc.WithUnaryInterceptor(bearerInterceptor(cfg.Token)),
		)

	case cfg.CACert != "" && cfg.Cert != "" && cfg.Key != "":
		// mTLS auth.
		cert, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		pool, err := certPool(cfg.CACert)
		if err != nil {
			return nil, err
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
			MinVersion:   tls.VersionTLS12,
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))

	default:
		// Fall back to system trust store with no client certs.
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	}

	conn, err := grpc.NewClient(cfg.GatewayURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("dialing gateway %s: %w", cfg.GatewayURL, err)
	}

	return &Client{
		conn:      conn,
		OpenShell: pb.NewOpenShellClient(conn),
		Inference: pb.NewInferenceClient(conn),
	}, nil
}

// Close tears down the gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// certPool reads a PEM CA bundle from disk.
func certPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading CA certificate %s: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA certificate %s contains no valid certs", path)
	}
	return pool, nil
}

// bearerInterceptor injects an Authorization header on every unary call.
func bearerInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// IsNotFound returns true when the gRPC error has status code NotFound.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	return ok && st.Code() == codes.NotFound
}
