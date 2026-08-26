package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &PostgresqlRoleResource{}

func NewPostgresqlRoleResource() resource.Resource {
	return &PostgresqlRoleResource{}
}

type PostgresqlRoleResource struct {
	client *Client
}

type PostgresqlRoleResourceModel struct {
	Name              types.String `tfsdk:"name"`
	Login             types.Bool   `tfsdk:"login"`
	Superuser         types.Bool   `tfsdk:"superuser"`
	CreateRole        types.Bool   `tfsdk:"create_role"`
	CreateDatabase    types.Bool   `tfsdk:"create_database"`
	ConnectionLimit   types.Int64  `tfsdk:"connection_limit"`
	ValidUntil        types.String `tfsdk:"valid_until"`
	Roles             types.List   `tfsdk:"roles"`
	PasswordWo        types.String `tfsdk:"password_wo"`
	PasswordWoVersion types.Int64  `tfsdk:"password_wo_version"`
}

func (r *PostgresqlRoleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *PostgresqlRoleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *PostgresqlRoleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A PostgreSQL role (CREATE ROLE).",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Role name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"login": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the role can log in. Defaults to false (matches PostgreSQL's own CREATE ROLE default).",
			},
			"superuser": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the role is a superuser.",
			},
			"create_role": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the role can create other roles.",
			},
			"create_database": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the role can create databases.",
			},
			"connection_limit": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(-1),
				Description: "Maximum concurrent connections for the role. -1 (default) means no limit.",
			},
			"valid_until": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Timestamp after which the role's password is no longer valid. Defaults to \"infinity\" when unset.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"roles": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Roles this role is a member of (native PostgreSQL role membership, GRANT role TO role).",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"password_wo": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				WriteOnly:   true,
				Description: "Write-only password for the role. Never stored in state.",
			},
			"password_wo_version": schema.Int64Attribute{
				Required:    true,
				Description: "Version of password_wo. Changing this value rotates the password, even if password_wo's value is unchanged - Terraform can never diff a write-only value, so this is the only usable rotation signal.",
			},
		},
	}
}

func (r *PostgresqlRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

func (r *PostgresqlRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, config PostgresqlRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	password := config.PasswordWo.ValueString()
	memberOf := stringListToGo(plan.Roles)

	tflog.Trace(ctx, "creating postgresql role", map[string]interface{}{"role": name})

	validUntilSet := !plan.ValidUntil.IsNull() && !plan.ValidUntil.IsUnknown()
	attrs := roleAttributesClause(&plan, password, validUntilSet)
	createSQL := fmt.Sprintf("CREATE ROLE %s WITH %s", quoteIdent(name), attrs)

	err := r.client.WithCatalogLock(ctx, func(ctx context.Context, tx pgx.Tx) error {
		attempt, err := tx.Begin(ctx)
		if err != nil {
			return fmt.Errorf("could not start role creation attempt: %w", err)
		}
		_, createErr := attempt.Exec(ctx, createSQL)
		if createErr == nil {
			if err := attempt.Commit(ctx); err != nil {
				return fmt.Errorf("could not create role: %w", err)
			}
		} else {
			_ = attempt.Rollback(ctx)
			if !isPgErrorCode(createErr, pgErrDuplicateObject) {
				return fmt.Errorf("could not create role: %w", createErr)
			}
			alterSQL := fmt.Sprintf("ALTER ROLE %s WITH %s", quoteIdent(name), attrs)
			if _, err := tx.Exec(ctx, alterSQL); err != nil {
				return fmt.Errorf("role %q already existed and could not be updated to match configuration: %w", name, err)
			}
		}
		for _, parent := range memberOf {
			if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT %s TO %s", quoteIdent(parent), quoteIdent(name))); err != nil {
				return fmt.Errorf("could not grant membership in %q: %w", parent, err)
			}
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating PostgreSQL Role", fmt.Sprintf("Could not create role %s: %s", name, err.Error()))
		return
	}

	if !validUntilSet {
		plan.ValidUntil = types.StringValue("infinity")
	}
	if plan.Roles.IsNull() || plan.Roles.IsUnknown() {
		plan.Roles = stringListValue(memberOf)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PostgresqlRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	tflog.Trace(ctx, "reading postgresql role", map[string]interface{}{"role": name})

	row := r.client.QueryRow(ctx, `
		SELECT rolcanlogin, rolsuper, rolcreaterole, rolcreatedb, rolconnlimit,
		       COALESCE(rolvaliduntil::text, 'infinity')
		FROM pg_catalog.pg_roles WHERE rolname = $1`, name)

	var login, superuser, createRole, createDatabase bool
	var connLimit int64
	var validUntil string
	if err := row.Scan(&login, &superuser, &createRole, &createDatabase, &connLimit, &validUntil); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			tflog.Warn(ctx, "postgresql role not found, removing from state", map[string]interface{}{"role": name})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading PostgreSQL Role", fmt.Sprintf("Could not read role %s: %s", name, err.Error()))
		return
	}

	memberOf, err := r.readMembership(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading PostgreSQL Role", fmt.Sprintf("Could not read role membership for %s: %s", name, err.Error()))
		return
	}

	state.Login = types.BoolValue(login)
	state.Superuser = types.BoolValue(superuser)
	state.CreateRole = types.BoolValue(createRole)
	state.CreateDatabase = types.BoolValue(createDatabase)
	state.ConnectionLimit = types.Int64Value(connLimit)
	state.ValidUntil = types.StringValue(validUntil)
	state.Roles = stringListValue(memberOf)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PostgresqlRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state, config PostgresqlRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	tflog.Trace(ctx, "updating postgresql role", map[string]interface{}{"role": name})

	rotatePassword := plan.PasswordWoVersion.ValueInt64() != state.PasswordWoVersion.ValueInt64()
	password := ""
	if rotatePassword {
		password = config.PasswordWo.ValueString()
	}
	validUntilSet := !plan.ValidUntil.IsNull() && !plan.ValidUntil.IsUnknown()
	sql := fmt.Sprintf("ALTER ROLE %s WITH %s", quoteIdent(name), roleAttributesClause(&plan, password, validUntilSet))

	desired := stringListToGo(plan.Roles)
	current := stringListToGo(state.Roles)
	toGrant, toRevoke := diffStringLists(current, desired)

	err := r.client.WithCatalogLock(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, sql); err != nil {
			return fmt.Errorf("could not alter role: %w", err)
		}
		for _, parent := range toGrant {
			if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT %s TO %s", quoteIdent(parent), quoteIdent(name))); err != nil {
				return fmt.Errorf("could not grant membership in %q: %w", parent, err)
			}
		}
		for _, parent := range toRevoke {
			if _, err := tx.Exec(ctx, fmt.Sprintf("REVOKE %s FROM %s", quoteIdent(parent), quoteIdent(name))); err != nil {
				return fmt.Errorf("could not revoke membership in %q: %w", parent, err)
			}
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating PostgreSQL Role", fmt.Sprintf("Could not update role %s: %s", name, err.Error()))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PostgresqlRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	tflog.Trace(ctx, "deleting postgresql role", map[string]interface{}{"role": name})

	err := r.client.WithCatalogLock(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, fmt.Sprintf("DROP ROLE %s", quoteIdent(name)))
		return err
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting PostgreSQL Role", fmt.Sprintf("Could not delete role %s: %s", name, err.Error()))
		return
	}
}

func (r *PostgresqlRoleResource) readMembership(ctx context.Context, name string) ([]string, error) {
	rows, err := r.client.Query(ctx, `
		SELECT parent.rolname
		FROM pg_catalog.pg_auth_members m
		JOIN pg_catalog.pg_roles member ON member.oid = m.member
		JOIN pg_catalog.pg_roles parent ON parent.oid = m.roleid
		WHERE member.rolname = $1
		ORDER BY parent.rolname`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var parent string
		if err := rows.Scan(&parent); err != nil {
			return nil, err
		}
		out = append(out, parent)
	}
	return out, rows.Err()
}

func boolOpt(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}

func roleAttributesClause(m *PostgresqlRoleResourceModel, password string, validUntilSet bool) string {
	clause := fmt.Sprintf(
		"%s %s %s %s CONNECTION LIMIT %d",
		boolOpt(m.Login.ValueBool(), "LOGIN", "NOLOGIN"),
		boolOpt(m.Superuser.ValueBool(), "SUPERUSER", "NOSUPERUSER"),
		boolOpt(m.CreateRole.ValueBool(), "CREATEROLE", "NOCREATEROLE"),
		boolOpt(m.CreateDatabase.ValueBool(), "CREATEDB", "NOCREATEDB"),
		m.ConnectionLimit.ValueInt64(),
	)
	if password != "" {
		clause += " PASSWORD " + quoteLiteral(password)
	}
	if validUntilSet {
		clause += " VALID UNTIL " + quoteLiteral(m.ValidUntil.ValueString())
	}
	return clause
}

func stringListToGo(list types.List) []string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var out []string
	for _, v := range list.Elements() {
		if s, ok := v.(types.String); ok {
			out = append(out, s.ValueString())
		}
	}
	return out
}

func stringListValue(items []string) types.List {
	values := make([]attr.Value, 0, len(items))
	for _, s := range items {
		values = append(values, types.StringValue(s))
	}
	return types.ListValueMust(types.StringType, values)
}

func diffStringLists(current, desired []string) (toAdd, toRemove []string) {
	currentSet := make(map[string]bool, len(current))
	for _, s := range current {
		currentSet[s] = true
	}
	desiredSet := make(map[string]bool, len(desired))
	for _, s := range desired {
		desiredSet[s] = true
	}
	for _, s := range desired {
		if !currentSet[s] {
			toAdd = append(toAdd, s)
		}
	}
	for _, s := range current {
		if !desiredSet[s] {
			toRemove = append(toRemove, s)
		}
	}
	return toAdd, toRemove
}
