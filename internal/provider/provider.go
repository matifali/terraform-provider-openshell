// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nvidia/terraform-provider-openshell/internal/client"
	"github.com/nvidia/terraform-provider-openshell/internal/datasources"
	"github.com/nvidia/terraform-provider-openshell/internal/resources"
)

var _ provider.Provider = &OpenShellProvider{}

// OpenShellProvider implements the Terraform provider for NVIDIA OpenShell.
type OpenShellProvider struct {
	version string
}

// OpenShellProviderModel maps the provider HCL block.
type OpenShellProviderModel struct {
	GatewayURL types.String `tfsdk:"gateway_url"`
	CACert     types.String `tfsdk:"ca_cert"`
	Cert       types.String `tfsdk:"cert"`
	Key        types.String `tfsdk:"key"`
	Token      types.String `tfsdk:"token"`
	Insecure   types.Bool   `tfsdk:"insecure"`
}

func (p *OpenShellProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "openshell"
	resp.Version = p.version
}

func (p *OpenShellProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The OpenShell provider manages [NVIDIA OpenShell](https://github.com/NVIDIA/OpenShell) " +
			"sandboxes, credential providers, and inference routing via the gateway gRPC API.",
		Attributes: map[string]schema.Attribute{
			"gateway_url": schema.StringAttribute{
				MarkdownDescription: "gRPC endpoint of the OpenShell gateway (e.g. `localhost:8443`). " +
					"Can also be set with the `OPENSHELL_GATEWAY_URL` environment variable.",
				Optional: true,
			},
			"ca_cert": schema.StringAttribute{
				MarkdownDescription: "Path to the CA certificate for mTLS. " +
					"Can also be set with `OPENSHELL_CA_CERT`.",
				Optional: true,
			},
			"cert": schema.StringAttribute{
				MarkdownDescription: "Path to the client certificate for mTLS. " +
					"Can also be set with `OPENSHELL_CERT`.",
				Optional: true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Path to the client private key for mTLS. " +
					"Can also be set with `OPENSHELL_KEY`.",
				Optional:  true,
				Sensitive: true,
			},
			"token": schema.StringAttribute{
				MarkdownDescription: "Bearer token for edge/remote gateway authentication. " +
					"Can also be set with `OPENSHELL_TOKEN`. When set, mTLS fields are ignored.",
				Optional:  true,
				Sensitive: true,
			},
			"insecure": schema.BoolAttribute{
				MarkdownDescription: "Disable TLS entirely. Only use for local development.",
				Optional: true,
			},
		},
	}
}

func (p *OpenShellProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data OpenShellProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := client.Config{
		GatewayURL: stringValueOrEnv(data.GatewayURL, "OPENSHELL_GATEWAY_URL"),
		CACert:     stringValueOrEnv(data.CACert, "OPENSHELL_CA_CERT"),
		Cert:       stringValueOrEnv(data.Cert, "OPENSHELL_CERT"),
		Key:        stringValueOrEnv(data.Key, "OPENSHELL_KEY"),
		Token:      stringValueOrEnv(data.Token, "OPENSHELL_TOKEN"),
		Insecure:   data.Insecure.ValueBool(),
	}

	if cfg.GatewayURL == "" {
		resp.Diagnostics.AddError(
			"Missing Gateway URL",
			"Set gateway_url in the provider block or OPENSHELL_GATEWAY_URL in the environment.",
		)
		return
	}

	c, err := client.New(ctx, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Gateway Connection Error", err.Error())
		return
	}

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *OpenShellProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewSandboxResource,
		resources.NewProviderResource,
		resources.NewInferenceResource,
	}
}

func (p *OpenShellProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		datasources.NewSandboxDataSource,
		datasources.NewProviderDataSource,
		datasources.NewSandboxesDataSource,
		datasources.NewProvidersDataSource,
	}
}

// New returns a factory function that Terraform calls to instantiate the
// provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &OpenShellProvider{version: version}
	}
}

// stringValueOrEnv returns the Terraform attribute value if set, otherwise
// falls back to the named environment variable.
func stringValueOrEnv(v types.String, envKey string) string {
	if !v.IsNull() && !v.IsUnknown() {
		return v.ValueString()
	}
	return os.Getenv(envKey)
}
