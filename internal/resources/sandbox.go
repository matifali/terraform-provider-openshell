// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
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
	_ resource.Resource                = &SandboxResource{}
	_ resource.ResourceWithConfigure   = &SandboxResource{}
	_ resource.ResourceWithImportState = &SandboxResource{}
)

// NewSandboxResource returns a factory function for the sandbox resource.
func NewSandboxResource() resource.Resource {
	return &SandboxResource{}
}

// SandboxResource manages an OpenShell sandbox via the gateway gRPC API.
type SandboxResource struct {
	client *client.Client
}

// SandboxResourceModel maps the Terraform state for openshell_sandbox.
type SandboxResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Image       types.String `tfsdk:"image"`
	Providers   types.List   `tfsdk:"providers"`
	Environment types.Map    `tfsdk:"environment"`
	GPU         types.Bool   `tfsdk:"gpu"`
	LogLevel    types.String `tfsdk:"log_level"`
	Phase       types.String `tfsdk:"phase"`
	Namespace   types.String `tfsdk:"namespace"`
}

func (r *SandboxResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sandbox"
}

func (r *SandboxResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an OpenShell sandbox. Sandboxes are immutable — any " +
			"spec change destroys and recreates the resource.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Server-assigned UUID of the sandbox.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Unique name for the sandbox. If omitted the " +
					"server generates one.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "Container image used by the sandbox.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"providers": schema.ListAttribute{
				MarkdownDescription: "Credential provider names attached to the sandbox.",
				Optional:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"environment": schema.MapAttribute{
				MarkdownDescription: "Environment variables injected into the sandbox.",
				Optional:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Map{
					mapplanmodifier.RequiresReplace(),
				},
			},
			"gpu": schema.BoolAttribute{
				MarkdownDescription: "Enable GPU passthrough for the sandbox.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"log_level": schema.StringAttribute{
				MarkdownDescription: "Log level for the sandbox process " +
					"(e.g. `debug`, `info`, `warn`, `error`).",
				Optional: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"phase": schema.StringAttribute{
				MarkdownDescription: "Current lifecycle phase reported by the server.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"namespace": schema.StringAttribute{
				MarkdownDescription: "Kubernetes namespace the sandbox runs in.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *SandboxResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Provider Data",
			fmt.Sprintf("Expected *client.Client, got %T", req.ProviderData),
		)
		return
	}
	r.client = c
}

func (r *SandboxResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SandboxResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec := &pb.SandboxSpec{
		Template: &pb.SandboxTemplate{
			Image: plan.Image.ValueString(),
		},
		Gpu: plan.GPU.ValueBool(),
	}

	if !plan.LogLevel.IsNull() && !plan.LogLevel.IsUnknown() {
		spec.LogLevel = plan.LogLevel.ValueString()
	}

	// Flatten providers list.
	if !plan.Providers.IsNull() && !plan.Providers.IsUnknown() {
		var providers []string
		resp.Diagnostics.Append(plan.Providers.ElementsAs(ctx, &providers, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		spec.Providers = providers
	}

	// Flatten environment map.
	if !plan.Environment.IsNull() && !plan.Environment.IsUnknown() {
		env := make(map[string]string)
		resp.Diagnostics.Append(plan.Environment.ElementsAs(ctx, &env, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		spec.Environment = env
	}

	grpcResp, err := r.client.OpenShell.CreateSandbox(ctx, &pb.CreateSandboxRequest{
		Spec: spec,
		Name: plan.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Sandbox", err.Error())
		return
	}

	mapSandboxToState(ctx, grpcResp.GetSandbox(), &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SandboxResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SandboxResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	grpcResp, err := r.client.OpenShell.GetSandbox(ctx, &pb.GetSandboxRequest{
		Name: state.Name.ValueString(),
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// The sandbox was deleted out-of-band; remove it from
			// state so Terraform knows to recreate it.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Sandbox", err.Error())
		return
	}

	mapSandboxToState(ctx, grpcResp.GetSandbox(), &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is a no-op because every mutable attribute carries ForceNew.
// Terraform will never call this — it destroys and recreates instead.
func (r *SandboxResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update Not Supported",
		"Sandboxes are immutable. This is a provider bug — Terraform should "+
			"have planned a destroy-then-create instead.",
	)
}

func (r *SandboxResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SandboxResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.OpenShell.DeleteSandbox(ctx, &pb.DeleteSandboxRequest{
		Name: state.Name.ValueString(),
	})
	if err != nil {
		// Treat already-deleted as success so `terraform destroy`
		// stays idempotent.
		if status.Code(err) == codes.NotFound {
			return
		}
		resp.Diagnostics.AddError("Error Deleting Sandbox", err.Error())
	}
}

func (r *SandboxResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// mapSandboxToState writes the gRPC Sandbox message into the Terraform
// state model.  Shared by Create and Read to avoid duplication.
func mapSandboxToState(
	ctx context.Context,
	sb *pb.Sandbox,
	model *SandboxResourceModel,
	diags *diag.Diagnostics,
) {
	if sb == nil {
		diags.AddError("Empty Response", "The server returned a nil sandbox.")
		return
	}

	model.ID = types.StringValue(sb.GetId())
	model.Name = types.StringValue(sb.GetName())
	model.Namespace = types.StringValue(sb.GetNamespace())
	model.Phase = types.StringValue(sb.GetPhase().String())

	spec := sb.GetSpec()
	if spec != nil {
		if tpl := spec.GetTemplate(); tpl != nil {
			model.Image = types.StringValue(tpl.GetImage())
		}
		model.GPU = types.BoolValue(spec.GetGpu())

		if spec.GetLogLevel() != "" {
			model.LogLevel = types.StringValue(spec.GetLogLevel())
		}

		// Providers — preserve Null when the server returns an
		// empty list so Terraform doesn't see a spurious diff.
		if len(spec.GetProviders()) > 0 {
			pv, d := types.ListValueFrom(ctx, types.StringType, spec.GetProviders())
			diags.Append(d...)
			model.Providers = pv
		} else if model.Providers.IsNull() {
			model.Providers = types.ListNull(types.StringType)
		}

		// Environment — same Null-preservation logic.
		if len(spec.GetEnvironment()) > 0 {
			ev, d := types.MapValueFrom(ctx, types.StringType, spec.GetEnvironment())
			diags.Append(d...)
			model.Environment = ev
		} else if model.Environment.IsNull() {
			model.Environment = types.MapNull(types.StringType)
		}
	}
}
