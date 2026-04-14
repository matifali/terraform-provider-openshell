// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nvidia/terraform-provider-openshell/internal/client"
	pb "github.com/nvidia/terraform-provider-openshell/proto/openshellv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Compile-time interface checks.
var (
	_ resource.Resource                = &providerResource{}
	_ resource.ResourceWithConfigure   = &providerResource{}
	_ resource.ResourceWithImportState = &providerResource{}
)

// NewProviderResource returns a factory function for the
// openshell_provider resource.
func NewProviderResource() resource.Resource {
	return &providerResource{}
}

// providerResource manages an OpenShell credential provider.
type providerResource struct {
	client *client.Client
}

// providerResourceModel maps the Terraform schema to Go types.
type providerResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Type        types.String `tfsdk:"type"`
	Credentials types.Map    `tfsdk:"credentials"`
	Config      types.Map    `tfsdk:"config"`
}

func (r *providerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (r *providerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an OpenShell credential provider.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the provider.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Unique name of the credential provider.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Provider type slug (e.g. `claude`, `openai`, `github`, `gitlab`, `generic`).",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"credentials": schema.MapAttribute{
				MarkdownDescription: "Secret key-value pairs such as API keys or tokens.",
				Optional:            true,
				Sensitive:           true,
				ElementType:         types.StringType,
			},
			"config": schema.MapAttribute{
				MarkdownDescription: "Non-secret configuration key-value pairs.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *providerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData),
		)
		return
	}

	r.client = c
}

func (r *providerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan providerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pbProvider, diags := modelToProto(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.OpenShell.CreateProvider(ctx, &pb.CreateProviderRequest{
		Provider: pbProvider,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Provider", err.Error())
		return
	}

	// Map server response but preserve planned credentials — the
	// server typically doesn't echo secret values back.
	plannedCreds := plan.Credentials
	protoToModel(result.Provider, &plan)
	plan.Credentials = plannedCreds
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *providerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state providerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.OpenShell.GetProvider(ctx, &pb.GetProviderRequest{
		Name: state.Name.ValueString(),
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Provider", err.Error())
		return
	}

	// Preserve credentials from state — server doesn't echo secrets.
	savedCreds := state.Credentials
	protoToModel(result.Provider, &state)
	state.Credentials = savedCreds
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *providerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan providerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Preserve the server-assigned ID from state.
	var state providerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID

	pbProvider, diags := modelToProto(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	pbProvider.Id = state.ID.ValueString()

	result, err := r.client.OpenShell.UpdateProvider(ctx, &pb.UpdateProviderRequest{
		Provider: pbProvider,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Provider", err.Error())
		return
	}

	// Preserve credentials from plan — server doesn't echo secrets.
	plannedCreds := plan.Credentials
	protoToModel(result.Provider, &plan)
	plan.Credentials = plannedCreds
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *providerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state providerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.OpenShell.DeleteProvider(ctx, &pb.DeleteProviderRequest{
		Name: state.Name.ValueString(),
	})
	if err != nil {
		// Treat already-deleted resources as a successful delete.
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Provider", err.Error())
	}
}

func (r *providerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// modelToProto converts a Terraform resource model into a protobuf
// Provider message suitable for create and update calls.
func modelToProto(ctx context.Context, m *providerResourceModel) (*pb.Provider, diag.Diagnostics) {
	var diags diag.Diagnostics

	p := &pb.Provider{
		Name: m.Name.ValueString(),
		Type: m.Type.ValueString(),
	}

	if !m.Credentials.IsNull() && !m.Credentials.IsUnknown() {
		creds := make(map[string]string)
		diags.Append(m.Credentials.ElementsAs(ctx, &creds, false)...)
		p.Credentials = creds
	}

	if !m.Config.IsNull() && !m.Config.IsUnknown() {
		cfg := make(map[string]string)
		diags.Append(m.Config.ElementsAs(ctx, &cfg, false)...)
		p.Config = cfg
	}

	return p, diags
}

// protoToModel writes protobuf Provider fields back into the Terraform
// resource model. Credentials are sensitive and may not be echoed by
// the server, so we preserve whatever the plan/state already holds
// unless the server explicitly returns values.
func protoToModel(p *pb.Provider, m *providerResourceModel) {
	m.ID = types.StringValue(p.Id)
	m.Name = types.StringValue(p.Name)
	m.Type = types.StringValue(p.Type)

	if len(p.Config) > 0 {
		elements := make(map[string]attr.Value, len(p.Config))
		for k, v := range p.Config {
			elements[k] = types.StringValue(v)
		}
		m.Config = types.MapValueMust(types.StringType, elements)
	} else if !m.Config.IsNull() {
		// Server returned empty config but state had a value — reset
		// to null to avoid a spurious diff.
		m.Config = types.MapNull(types.StringType)
	}

	if len(p.Credentials) > 0 {
		elements := make(map[string]attr.Value, len(p.Credentials))
		for k, v := range p.Credentials {
			elements[k] = types.StringValue(v)
		}
		m.Credentials = types.MapValueMust(types.StringType, elements)
	}
	// If the server didn't echo credentials, keep whatever the
	// plan/state already holds — the Sensitive flag prevents
	// Terraform from showing a diff.
}
