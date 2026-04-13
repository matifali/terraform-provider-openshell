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
	_ datasource.DataSource              = &providersDataSource{}
	_ datasource.DataSourceWithConfigure = &providersDataSource{}
)

// providersDataSource lists all credential providers.
type providersDataSource struct {
	client *client.Client
}

type providersDataSourceModel struct {
	Providers []providerItemModel `tfsdk:"providers"`
}

type providerItemModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Type types.String `tfsdk:"type"`
}

func NewProvidersDataSource() datasource.DataSource {
	return &providersDataSource{}
}

func (d *providersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_providers"
}

func (d *providersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all OpenShell credential providers.",
		Attributes: map[string]schema.Attribute{
			"providers": schema.ListNestedAttribute{
				Description: "The list of credential providers.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "The unique identifier of the credential provider.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "The name of the credential provider.",
							Computed:    true,
						},
						"type": schema.StringAttribute{
							Description: "The type of the credential provider (e.g. ngc, github).",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *providersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *providersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	result, err := d.client.OpenShell.ListProviders(ctx, &pb.ListProvidersRequest{})
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to List Providers",
			fmt.Sprintf("Could not list providers: %s", err),
		)
		return
	}

	var state providersDataSourceModel
	for _, p := range result.GetProviders() {
		state.Providers = append(state.Providers, providerItemModel{
			ID:   types.StringValue(p.GetId()),
			Name: types.StringValue(p.GetName()),
			Type: types.StringValue(p.GetType()),
		})
	}

	// Ensure the list is never null in state, even when the API
	// returns zero providers.
	if state.Providers == nil {
		state.Providers = []providerItemModel{}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
