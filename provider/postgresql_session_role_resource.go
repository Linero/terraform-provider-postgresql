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
	_ resource.Resource                = &PostgresqlSessionRoleResource{}
	_ resource.ResourceWithImportState = &PostgresqlSessionRoleResource{}
)

func NewPostgresqlSessionRoleResource() resource.Resource {
	return &PostgresqlSessionRoleResource{}
}

type PostgresqlSessionRoleResource struct {
	client *Client
}

type PostgresqlSessionRoleResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Role        types.String `tfsdk:"role"`
	Database    types.String `tfsdk:"database"`
	SessionRole types.String `tfsdk:"session_role"`
}

func (r *PostgresqlSessionRoleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *PostgresqlSessionRoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_session_role"
}

func (r *PostgresqlSessionRoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sets the role a login role's sessions automatically switch to on connect " +
			"(ALTER ROLE role [IN DATABASE database] SET role = session_role) - every session then runs as " +
			"session_role, exactly as if it had run SET ROLE itself, so e.g. objects it creates are owned by " +
			"session_role. role must be a member of session_role (see postgresql_grant_role).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role": schema.StringAttribute{
				Required:    true,
				Description: "Login role whose sessions switch role.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"database": schema.StringAttribute{
				Optional:    true,
				Description: "Only switch role in sessions connected to this database. If unset, applies to every database.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"session_role": schema.StringAttribute{
				Required:    true,
				Description: "Role the sessions switch to.",
			},
		},
	}
}

func (r *PostgresqlSessionRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	role, database, _ := strings.Cut(req.ID, "|")
	if role == "" {
		resp.Diagnostics.AddError("Invalid Import ID", fmt.Sprintf("Expected \"<role>\" or \"<role>|<database>\", got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), role)...)
	if database != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("database"), database)...)
	}
}

func (r *PostgresqlSessionRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PostgresqlSessionRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error Creating PostgreSQL Session Role", err.Error())
		return
	}

	plan.ID = types.StringValue(sessionRoleID(plan.Role.ValueString(), plan.Database.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlSessionRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PostgresqlSessionRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.apply(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error Updating PostgreSQL Session Role", err.Error())
		return
	}

	plan.ID = types.StringValue(sessionRoleID(plan.Role.ValueString(), plan.Database.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlSessionRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PostgresqlSessionRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, database := state.Role.ValueString(), state.Database.ValueString()
	tflog.Trace(ctx, "reading postgresql session role", map[string]interface{}{"role": role, "database": database})

	// setdatabase = 0 is the "every database" entry. A missing role or database yields no row.
	var settings []string
	err := r.client.QueryRow(ctx, `
		SELECT s.setconfig
		FROM pg_catalog.pg_db_role_setting s
		JOIN pg_catalog.pg_roles r ON r.oid = s.setrole
		WHERE r.rolname = $1
		  AND s.setdatabase = CASE WHEN $2::text = '' THEN 0
		                           ELSE (SELECT oid FROM pg_catalog.pg_database WHERE datname = $2::text) END`,
		role, database).Scan(&settings)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		resp.Diagnostics.AddError("Error Reading PostgreSQL Session Role",
			fmt.Sprintf("Could not read settings of role %s: %s", role, err.Error()))
		return
	}

	sessionRole, found := sessionRoleSetting(settings)
	if !found {
		tflog.Warn(ctx, "postgresql session role not found, removing from state", map[string]interface{}{"role": role, "database": database})
		resp.State.RemoveResource(ctx)
		return
	}

	state.SessionRole = types.StringValue(sessionRole)
	state.ID = types.StringValue(sessionRoleID(role, database))
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PostgresqlSessionRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PostgresqlSessionRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, database := state.Role.ValueString(), state.Database.ValueString()
	tflog.Trace(ctx, "deleting postgresql session role", map[string]interface{}{"role": role, "database": database})

	sql := fmt.Sprintf("ALTER ROLE %s%s RESET role", quoteIdent(role), inDatabaseClause(database))
	err := r.client.WithCatalogLock(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
	if err != nil {
		if isPgErrorCode(err, pgErrUndefinedObject) || isDatabaseMissing(err) {
			tflog.Trace(ctx, "postgresql role or database no longer exists, nothing to reset", map[string]interface{}{"role": role, "database": database})
			return
		}
		resp.Diagnostics.AddError("Error Deleting PostgreSQL Session Role",
			fmt.Sprintf("Could not reset session role of %s: %s", role, err.Error()))
		return
	}
}

func (r *PostgresqlSessionRoleResource) apply(ctx context.Context, m *PostgresqlSessionRoleResourceModel) error {
	role, database, sessionRole := m.Role.ValueString(), m.Database.ValueString(), m.SessionRole.ValueString()
	tflog.Trace(ctx, "setting postgresql session role", map[string]interface{}{"role": role, "database": database, "session_role": sessionRole})

	sql := fmt.Sprintf("ALTER ROLE %s%s SET role = %s", quoteIdent(role), inDatabaseClause(database), quoteLiteral(sessionRole))
	err := r.client.WithCatalogLock(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
	if err != nil {
		return fmt.Errorf("could not set session role of %s to %s: %w", role, sessionRole, err)
	}
	return nil
}

func inDatabaseClause(database string) string {
	if database == "" {
		return ""
	}
	return " IN DATABASE " + quoteIdent(database)
}

func sessionRoleID(role, database string) string {
	if database == "" {
		return role
	}
	return role + "|" + database
}

// sessionRoleSetting extracts the "role" entry from a pg_db_role_setting.setconfig array,
// whose entries are stored as "name=value".
func sessionRoleSetting(settings []string) (string, bool) {
	for _, s := range settings {
		if name, value, ok := strings.Cut(s, "="); ok && name == "role" {
			return value, true
		}
	}
	return "", false
}
