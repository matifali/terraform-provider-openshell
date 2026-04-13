// SPDX-License-Identifier: MPL-2.0

package datasources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nvidia/terraform-provider-openshell/internal/client"
	pb "github.com/nvidia/terraform-provider-openshell/proto/openshellv1"
)

var (
	_ datasource.DataSource              = &providerDataSource{}
	_ datasource.DataSourceWithConfigure = &providerDataSource{}
)

func NewProviderDataSource() datasource.DataSource {
	return &providerDataSource{}
}

type providerDataSource struct {
	client *client.Client
}

// providerDataSourceModel exposes non-secret provider metadata.
// Credentials are intentionally excluded for security.
type providerDataSourceModel struct {
	Name   types.String `tfsdk:"name"`
	ID     types.String `tfsdk:"id"`
	Type   types.String `tfsdk:"type"`
	Config types.Map    `tfsdk:"config"`
}

func (d *providerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (d *providerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single OpenShell credential provider by name. " +
			"Credentials are not exposed for security reasons.",
		Attributes: map[string]schema.Attribute{
			"name":   schema.StringAttribute{Required: true, MarkdownDescription: "Provider name."},
			"id":     schema.StringAttribute{Computed: true, MarkdownDescription: "Server-assigned provider ID."},
			"type":   schema.StringAttribute{Computed: true, MarkdownDescription: "Provider type slug (claude, openai, github, etc.)."},
			"config": schema.MapAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Non-secret provider configuration."},
		},
	}
}

func (d *providerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
		return
	}
	d.client = c
}

func (d *providerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state providerDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	grpcResp, err := d.client.OpenShell.GetProvider(ctx, &pb.GetProviderRequest{
		Name: state.Name.ValueString(),
	})
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddWarning(
				"Provider Not Found",
				fmt.Sprintf("Provider %q was not found.", state.Name.ValueString()),
			)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to Read Provider", err.Error())
		return
	}

	p := grpcResp.GetProvider()
	if p == nil {
		resp.Diagnostics.AddError("Empty Response", "The server returned a nil provider.")
		return
	}

	state.ID = types.StringValue(p.GetId())
	state.Name = types.StringValue(p.GetName())
	state.Type = types.StringValue(p.GetType())

	if len(p.GetConfig()) > 0 {
		configMap, diags := types.MapValueFrom(ctx, types.StringType, p.GetConfig())
		resp.Diagnostics.Append(diags...)
		state.Config = configMap
	} else {
		state.Config = types.MapNull(types.StringType)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
