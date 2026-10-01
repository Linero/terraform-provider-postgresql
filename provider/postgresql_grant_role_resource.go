package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &PostgresqlGrantRoleResource{}
	_ resource.ResourceWithImportState = &PostgresqlGrantRoleResource{}
)

func NewPostgresqlGrantRoleResource() resource.Resource {
	return &PostgresqlGrantRoleResource{}
}

type PostgresqlGrantRoleResource struct {
	client *Client
}

type PostgresqlGrantRoleResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Role      types.String `tfsdk:"role"`
	GrantRole types.String `tfsdk:"grant_role"`
}

func (r *PostgresqlGrantRoleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *PostgresqlGrantRoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_grant_role"
}

func (r *PostgresqlGrantRoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Grants membership in one role to another (GRANT grant_role TO role), independent of " +
			"postgresql_role's own roles attribute - so the membership can be sequenced after both roles exist, " +
			"e.g. when both are created by the same for_each. Don't manage the same membership here and in " +
			"postgresql_role.roles at the same time.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role": schema.StringAttribute{
				Required:    true,
				Description: "Role that is granted membership (the member).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"grant_role": schema.StringAttribute{
				Required:    true,
				Description: "Role that role becomes a member of (the parent). role inherits its privileges.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *PostgresqlGrantRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	role, grantRole, ok := strings.Cut(req.ID, "|")
	if !ok || role == "" || grantRole == "" {
		resp.Diagnostics.AddError("Invalid Import ID", fmt.Sprintf("Expected \"<role>|<grant_role>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), role)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("grant_role"), grantRole)...)
}

func (r *PostgresqlGrantRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PostgresqlGrantRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, grantRole := plan.Role.ValueString(), plan.GrantRole.ValueString()
	tflog.Trace(ctx, "creating postgresql role membership", map[string]interface{}{"role": role, "grant_role": grantRole})

	// GRANT of an already existing membership only raises a NOTICE, so this also adopts
	// memberships created outside of Terraform (or by postgresql_role.roles) without failing.
	err := r.client.WithCatalogLock(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, fmt.Sprintf("GRANT %s TO %s", quoteIdent(grantRole), quoteIdent(role)))
		return err
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating PostgreSQL Role Membership",
			fmt.Sprintf("Could not grant %s to %s: %s", grantRole, role, err.Error()))
		return
	}

	plan.ID = types.StringValue(role + "|" + grantRole)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlGrantRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PostgresqlGrantRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, grantRole := state.Role.ValueString(), state.GrantRole.ValueString()
	tflog.Trace(ctx, "reading postgresql role membership", map[string]interface{}{"role": role, "grant_role": grantRole})

	err := r.client.QueryRow(ctx, `
		SELECT 1
		FROM pg_catalog.pg_auth_members m
		JOIN pg_catalog.pg_roles member ON member.oid = m.member
		JOIN pg_catalog.pg_roles parent ON parent.oid = m.roleid
		WHERE member.rolname = $1 AND parent.rolname = $2
		LIMIT 1`, role, grantRole).Scan(new(int))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			tflog.Warn(ctx, "postgresql role membership not found, removing from state", map[string]interface{}{"role": role, "grant_role": grantRole})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading PostgreSQL Role Membership",
			fmt.Sprintf("Could not read membership of %s in %s: %s", role, grantRole, err.Error()))
		return
	}

	state.ID = types.StringValue(role + "|" + grantRole)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called with a real change: every configurable attribute requires replacement.
func (r *PostgresqlGrantRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PostgresqlGrantRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlGrantRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PostgresqlGrantRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, grantRole := state.Role.ValueString(), state.GrantRole.ValueString()
	tflog.Trace(ctx, "deleting postgresql role membership", map[string]interface{}{"role": role, "grant_role": grantRole})

	err := r.client.WithCatalogLock(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, fmt.Sprintf("REVOKE %s FROM %s", quoteIdent(grantRole), quoteIdent(role)))
		return err
	})
	if err != nil {
		if isPgErrorCode(err, pgErrUndefinedObject) {
			tflog.Trace(ctx, "postgresql role no longer exists, nothing to revoke", map[string]interface{}{"role": role, "grant_role": grantRole})
			return
		}
		resp.Diagnostics.AddError("Error Deleting PostgreSQL Role Membership",
			fmt.Sprintf("Could not revoke %s from %s: %s", grantRole, role, err.Error()))
		return
	}
}
