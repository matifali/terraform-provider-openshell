// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/nvidia/terraform-provider-openshell/internal/client"
	pb "github.com/nvidia/terraform-provider-openshell/proto/openshellv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Compile-time interface checks.
var (
	_ resource.Resource              = &inferenceResource{}
	_ resource.ResourceWithConfigure = &inferenceResource{}
)

// NewInferenceResource returns a factory for the openshell_inference resource.
func NewInferenceResource() resource.Resource {
	return &inferenceResource{}
}

// inferenceResource manages cluster inference routing configuration via
// the OpenShell gateway gRPC API.
type inferenceResource struct {
	client *client.Client
}

// inferenceResourceModel maps the Terraform schema to Go types.
type inferenceResourceModel struct {
	ProviderName types.String `tfsdk:"provider_name"`
	ModelID      types.String `tfsdk:"model_id"`
	RouteName    types.String `tfsdk:"route_name"`
	TimeoutSecs  types.Int64  `tfsdk:"timeout_secs"`
	Version      types.Int64  `tfsdk:"version"`
}

func (r *inferenceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_inference"
}

func (r *inferenceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the OpenShell cluster inference routing configuration. " +
			"This controls how inference requests are routed to upstream providers.",
		Attributes: map[string]schema.Attribute{
			"provider_name": schema.StringAttribute{
				MarkdownDescription: "Name of the credential provider backing this inference route.",
				Required:            true,
			},
			"model_id": schema.StringAttribute{
				MarkdownDescription: "Model identifier to force on generation calls.",
				Required:            true,
			},
			"route_name": schema.StringAttribute{
				MarkdownDescription: "Route name to target. Defaults to `inference.local` (user-facing). " +
					"Use `sandbox-system` for the sandbox system-level inference route.",
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("inference.local"),
			},
			"timeout_secs": schema.Int64Attribute{
				MarkdownDescription: "Per-route request timeout in seconds.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(60),
			},
			"version": schema.Int64Attribute{
				MarkdownDescription: "Monotonic version incremented on every server-side update.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *inferenceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf(
				"Expected *client.Client, got: %T. Please report this issue to the provider developers.",
				req.ProviderData,
			),
		)
		return
	}

	r.client = c
}

func (r *inferenceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data inferenceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.Inference.SetClusterInference(ctx, &pb.SetClusterInferenceRequest{
		ProviderName: data.ProviderName.ValueString(),
		ModelId:      data.ModelID.ValueString(),
		RouteName:    data.RouteName.ValueString(),
		TimeoutSecs:  uint64(data.TimeoutSecs.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Inference Route", err.Error())
		return
	}

	data.ProviderName = types.StringValue(out.ProviderName)
	data.ModelID = types.StringValue(out.ModelId)
	data.RouteName = types.StringValue(out.RouteName)
	data.TimeoutSecs = types.Int64Value(int64(out.TimeoutSecs))
	data.Version = types.Int64Value(int64(out.Version))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *inferenceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data inferenceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.Inference.GetClusterInference(ctx, &pb.GetClusterInferenceRequest{
		RouteName: data.RouteName.ValueString(),
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			tflog.Warn(ctx, "Inference route not found on server, removing from state",
				map[string]interface{}{"route_name": data.RouteName.ValueString()})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Inference Route", err.Error())
		return
	}

	data.ProviderName = types.StringValue(out.ProviderName)
	data.ModelID = types.StringValue(out.ModelId)
	data.RouteName = types.StringValue(out.RouteName)
	data.TimeoutSecs = types.Int64Value(int64(out.TimeoutSecs))
	data.Version = types.Int64Value(int64(out.Version))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *inferenceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data inferenceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	out, err := r.client.Inference.SetClusterInference(ctx, &pb.SetClusterInferenceRequest{
		ProviderName: data.ProviderName.ValueString(),
		ModelId:      data.ModelID.ValueString(),
		RouteName:    data.RouteName.ValueString(),
		TimeoutSecs:  uint64(data.TimeoutSecs.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Inference Route", err.Error())
		return
	}

	data.ProviderName = types.StringValue(out.ProviderName)
	data.ModelID = types.StringValue(out.ModelId)
	data.RouteName = types.StringValue(out.RouteName)
	data.TimeoutSecs = types.Int64Value(int64(out.TimeoutSecs))
	data.Version = types.Int64Value(int64(out.Version))

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *inferenceResource) Delete(ctx context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
	// The OpenShell inference API has no delete RPC. Removing the
	// resource from state is sufficient; the server-side config
	// persists until overwritten by a new SetClusterInference call.
	tflog.Info(ctx, "Inference route has no delete API — removed from Terraform state only")
}
