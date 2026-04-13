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
	_ datasource.DataSource              = &sandboxDataSource{}
	_ datasource.DataSourceWithConfigure = &sandboxDataSource{}
)

func NewSandboxDataSource() datasource.DataSource {
	return &sandboxDataSource{}
}

type sandboxDataSource struct {
	client *client.Client
}

type sandboxDataSourceModel struct {
	Name        types.String `tfsdk:"name"`
	ID          types.String `tfsdk:"id"`
	Phase       types.String `tfsdk:"phase"`
	Namespace   types.String `tfsdk:"namespace"`
	Image       types.String `tfsdk:"image"`
	GPU         types.Bool   `tfsdk:"gpu"`
	Providers   types.List   `tfsdk:"providers"`
	Environment types.Map    `tfsdk:"environment"`
}

func (d *sandboxDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sandbox"
}

func (d *sandboxDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single OpenShell sandbox by name.",
		Attributes: map[string]schema.Attribute{
			"name":      schema.StringAttribute{Required: true, MarkdownDescription: "Sandbox name."},
			"id":        schema.StringAttribute{Computed: true, MarkdownDescription: "Server-assigned sandbox ID."},
			"phase":     schema.StringAttribute{Computed: true, MarkdownDescription: "Lifecycle phase (PROVISIONING, READY, ERROR, DELETING)."},
			"namespace": schema.StringAttribute{Computed: true, MarkdownDescription: "Kubernetes namespace."},
			"image":     schema.StringAttribute{Computed: true, MarkdownDescription: "Container image."},
			"gpu":       schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether GPU passthrough is enabled."},
			"providers": schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Attached credential provider names."},
			"environment": schema.MapAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Environment variables."},
		},
	}
}

func (d *sandboxDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *sandboxDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state sandboxDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	grpcResp, err := d.client.OpenShell.GetSandbox(ctx, &pb.GetSandboxRequest{
		Name: state.Name.ValueString(),
	})
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddWarning(
				"Sandbox Not Found",
				fmt.Sprintf("Sandbox %q was not found.", state.Name.ValueString()),
			)
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to Read Sandbox", err.Error())
		return
	}

	sb := grpcResp.GetSandbox()
	if sb == nil {
		resp.Diagnostics.AddError("Empty Response", "The server returned a nil sandbox.")
		return
	}

	state.ID = types.StringValue(sb.GetId())
	state.Name = types.StringValue(sb.GetName())
	state.Phase = types.StringValue(sb.GetPhase().String())
	state.Namespace = types.StringValue(sb.GetNamespace())

	spec := sb.GetSpec()
	if spec != nil {
		if tpl := spec.GetTemplate(); tpl != nil {
			state.Image = types.StringValue(tpl.GetImage())
		}
		state.GPU = types.BoolValue(spec.GetGpu())

		if len(spec.GetProviders()) > 0 {
			pv, diags := types.ListValueFrom(ctx, types.StringType, spec.GetProviders())
			resp.Diagnostics.Append(diags...)
			state.Providers = pv
		} else {
			state.Providers = types.ListNull(types.StringType)
		}

		if len(spec.GetEnvironment()) > 0 {
			ev, diags := types.MapValueFrom(ctx, types.StringType, spec.GetEnvironment())
			resp.Diagnostics.Append(diags...)
			state.Environment = ev
		} else {
			state.Environment = types.MapNull(types.StringType)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
