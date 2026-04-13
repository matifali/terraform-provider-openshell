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
	_ datasource.DataSource              = &sandboxesDataSource{}
	_ datasource.DataSourceWithConfigure = &sandboxesDataSource{}
)

func NewSandboxesDataSource() datasource.DataSource {
	return &sandboxesDataSource{}
}

type sandboxesDataSource struct {
	client *client.Client
}

type sandboxItemModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Phase     types.String `tfsdk:"phase"`
	Namespace types.String `tfsdk:"namespace"`
}

type sandboxesDataSourceModel struct {
	Sandboxes []sandboxItemModel `tfsdk:"sandboxes"`
}

func (d *sandboxesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sandboxes"
}

func (d *sandboxesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all OpenShell sandboxes on the active gateway.",
		Attributes: map[string]schema.Attribute{
			"sandboxes": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of sandboxes.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":        schema.StringAttribute{Computed: true},
						"name":      schema.StringAttribute{Computed: true},
						"phase":     schema.StringAttribute{Computed: true},
						"namespace": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *sandboxesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *sandboxesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	result, err := d.client.OpenShell.ListSandboxes(ctx, &pb.ListSandboxesRequest{})
	if err != nil {
		resp.Diagnostics.AddError("Unable to List Sandboxes", err.Error())
		return
	}

	var state sandboxesDataSourceModel
	for _, s := range result.GetSandboxes() {
		state.Sandboxes = append(state.Sandboxes, sandboxItemModel{
			ID:        types.StringValue(s.GetId()),
			Name:      types.StringValue(s.GetName()),
			Phase:     types.StringValue(s.GetPhase().String()),
			Namespace: types.StringValue(s.GetNamespace()),
		})
	}

	if state.Sandboxes == nil {
		state.Sandboxes = []sandboxItemModel{}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
