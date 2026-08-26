package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                   = &PostgresqlGrantResource{}
	_ resource.ResourceWithValidateConfig = &PostgresqlGrantResource{}
)

func NewPostgresqlGrantResource() resource.Resource {
	return &PostgresqlGrantResource{}
}

type PostgresqlGrantResource struct {
	client *Client
}

type PostgresqlGrantResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ObjectType      types.String `tfsdk:"object_type"`
	Database        types.String `tfsdk:"database"`
	Schema          types.String `tfsdk:"schema"`
	Role            types.String `tfsdk:"role"`
	Privileges      types.Set    `tfsdk:"privileges"`
	WithGrantOption types.Bool   `tfsdk:"with_grant_option"`
}

func (r *PostgresqlGrantResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *PostgresqlGrantResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_grant"
}

func (r *PostgresqlGrantResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Grants privileges on a database, schema, or all tables/sequences in a schema, to a role.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"object_type": schema.StringAttribute{
				Required:    true,
				Description: "One of: database, schema, table, sequence. table/sequence apply to ALL tables/sequences currently in the schema.",
				Validators: []validator.String{
					stringvalidator.OneOf(validObjectTypes...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"database": schema.StringAttribute{
				Required:    true,
				Description: "Database the grant applies to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"schema": schema.StringAttribute{
				Optional:    true,
				Description: "Schema the grant applies to. Required unless object_type = \"database\", and must be omitted when it is.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				Required:    true,
				Description: "Role the privileges are granted to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"privileges": schema.SetAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Privileges to grant, e.g. [\"SELECT\", \"INSERT\"].",
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.OneOf(validPrivileges...)),
				},
			},
			"with_grant_option": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
		},
	}
}

func (r *PostgresqlGrantResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data PostgresqlGrantResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.ObjectType.IsUnknown() || data.Schema.IsUnknown() {
		return
	}

	objectType := data.ObjectType.ValueString()
	schemaSet := !data.Schema.IsNull() && data.Schema.ValueString() != ""

	if objectType == "database" && schemaSet {
		resp.Diagnostics.AddAttributeError(path.Root("schema"), "Invalid Attribute Combination",
			`"schema" must not be set when object_type = "database".`)
	}
	if objectType != "database" && !schemaSet {
		resp.Diagnostics.AddAttributeError(path.Root("schema"), "Invalid Attribute Combination",
			`"schema" is required when object_type is "schema", "table", or "sequence".`)
	}
}

func (r *PostgresqlGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PostgresqlGrantResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	privileges := stringSetToGo(plan.Privileges)
	sql, err := grantSQL(plan.ObjectType.ValueString(), plan.Database.ValueString(), plan.Schema.ValueString(), plan.Role.ValueString(), privileges, plan.WithGrantOption.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Invalid Grant", err.Error())
		return
	}

	tflog.Trace(ctx, "creating postgresql grant", map[string]interface{}{"role": plan.Role.ValueString(), "database": plan.Database.ValueString()})

	err = r.client.WithCatalogLockOnDatabase(ctx, plan.Database.ValueString(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating PostgreSQL Grant", err.Error())
		return
	}

	plan.ID = types.StringValue(grantID(plan.ObjectType.ValueString(), plan.Database.ValueString(), plan.Schema.ValueString(), plan.Role.ValueString()))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PostgresqlGrantResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	objectType := state.ObjectType.ValueString()

	if objectType == "table" || objectType == "sequence" {
		roleExists, err := r.roleExists(ctx, state.Database.ValueString(), state.Role.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Error Reading PostgreSQL Grant", err.Error())
			return
		}
		if !roleExists {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	privileges, roleExists, err := r.readGrantedPrivileges(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading PostgreSQL Grant", err.Error())
		return
	}
	if !roleExists {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Privileges = stringSetValue(privileges)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PostgresqlGrantResource) roleExists(ctx context.Context, database, role string) (bool, error) {
	err := r.client.QueryRowOnDatabase(ctx, database, `SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1`, role).Scan(new(int))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || isDatabaseMissing(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *PostgresqlGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state PostgresqlGrantResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	desired := stringSetToGo(plan.Privileges)
	current := stringSetToGo(state.Privileges)
	toGrant, toRevoke := diffStringLists(current, desired)

	tflog.Trace(ctx, "updating postgresql grant", map[string]interface{}{"role": plan.Role.ValueString(), "database": plan.Database.ValueString()})

	err := r.client.WithCatalogLockOnDatabase(ctx, plan.Database.ValueString(), func(ctx context.Context, tx pgx.Tx) error {
		if len(toRevoke) > 0 {
			sql, err := revokeSQL(plan.ObjectType.ValueString(), plan.Database.ValueString(), plan.Schema.ValueString(), plan.Role.ValueString(), toRevoke)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, sql); err != nil {
				return fmt.Errorf("could not revoke removed privileges: %w", err)
			}
		}
		if len(toGrant) > 0 || plan.WithGrantOption.ValueBool() != state.WithGrantOption.ValueBool() {
			sql, err := grantSQL(plan.ObjectType.ValueString(), plan.Database.ValueString(), plan.Schema.ValueString(), plan.Role.ValueString(), desired, plan.WithGrantOption.ValueBool())
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, sql); err != nil {
				return fmt.Errorf("could not grant added/changed privileges: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating PostgreSQL Grant", err.Error())
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PostgresqlGrantResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	privileges := stringSetToGo(state.Privileges)
	sql, err := revokeSQL(state.ObjectType.ValueString(), state.Database.ValueString(), state.Schema.ValueString(), state.Role.ValueString(), privileges)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Grant", err.Error())
		return
	}

	tflog.Trace(ctx, "deleting postgresql grant", map[string]interface{}{"role": state.Role.ValueString(), "database": state.Database.ValueString()})

	err = r.client.WithCatalogLockOnDatabase(ctx, state.Database.ValueString(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
	if err != nil {
		if isDatabaseMissing(err) {
			tflog.Trace(ctx, "postgresql grant's database no longer exists, nothing to revoke", map[string]interface{}{"database": state.Database.ValueString()})
			return
		}
		resp.Diagnostics.AddError("Error Deleting PostgreSQL Grant", err.Error())
		return
	}
}

func (r *PostgresqlGrantResource) readGrantedPrivileges(ctx context.Context, state *PostgresqlGrantResourceModel) (privileges []string, roleExists bool, err error) {
	database := state.Database.ValueString()
	role := state.Role.ValueString()
	objectType := state.ObjectType.ValueString()

	exists, err := r.roleExists(ctx, database, role)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}

	var query string
	var args []any
	switch objectType {
	case "database":
		query = `
			SELECT priv.privilege_type
			FROM pg_catalog.pg_database d
			CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(d.datacl, pg_catalog.acldefault('d', d.datdba))) AS priv
			JOIN pg_catalog.pg_roles r ON r.oid = priv.grantee
			WHERE d.datname = $1 AND r.rolname = $2`
		args = []any{database, role}
	case "schema":
		query = `
			SELECT priv.privilege_type
			FROM pg_catalog.pg_namespace n
			CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(n.nspacl, pg_catalog.acldefault('n', n.nspowner))) AS priv
			JOIN pg_catalog.pg_roles r ON r.oid = priv.grantee
			WHERE n.nspname = $1 AND r.rolname = $2`
		args = []any{state.Schema.ValueString(), role}
	default:
		return nil, false, fmt.Errorf("unsupported object_type %q for live privilege lookup", objectType)
	}

	rows, err := r.client.QueryOnDatabase(ctx, database, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, false, err
		}
		privileges = append(privileges, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	return privileges, true, nil
}

func grantTargetClause(objectType, database, schemaName string) (string, error) {
	switch objectType {
	case "database":
		return fmt.Sprintf("DATABASE %s", quoteIdent(database)), nil
	case "schema":
		return fmt.Sprintf("SCHEMA %s", quoteIdent(schemaName)), nil
	case "table":
		return fmt.Sprintf("ALL TABLES IN SCHEMA %s", quoteIdent(schemaName)), nil
	case "sequence":
		return fmt.Sprintf("ALL SEQUENCES IN SCHEMA %s", quoteIdent(schemaName)), nil
	default:
		return "", fmt.Errorf("unsupported object_type %q", objectType)
	}
}

func grantSQL(objectType, database, schemaName, role string, privileges []string, withGrantOption bool) (string, error) {
	target, err := grantTargetClause(objectType, database, schemaName)
	if err != nil {
		return "", err
	}
	sql := fmt.Sprintf("GRANT %s ON %s TO %s", strings.Join(privileges, ", "), target, quoteIdent(role))
	if withGrantOption {
		sql += " WITH GRANT OPTION"
	}
	return sql, nil
}

func revokeSQL(objectType, database, schemaName, role string, privileges []string) (string, error) {
	target, err := grantTargetClause(objectType, database, schemaName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("REVOKE %s ON %s FROM %s", strings.Join(privileges, ", "), target, quoteIdent(role)), nil
}

func grantID(objectType, database, schemaName, role string) string {
	if objectType == "database" {
		return fmt.Sprintf("%s_%s_%s", role, database, objectType)
	}
	return fmt.Sprintf("%s_%s_%s_%s", role, database, schemaName, objectType)
}

func stringSetToGo(set types.Set) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var out []string
	for _, v := range set.Elements() {
		if s, ok := v.(types.String); ok {
			out = append(out, s.ValueString())
		}
	}
	sort.Strings(out)
	return out
}

func stringSetValue(items []string) types.Set {
	values := make([]attr.Value, 0, len(items))
	for _, s := range items {
		values = append(values, types.StringValue(s))
	}
	return types.SetValueMust(types.StringType, values)
}
