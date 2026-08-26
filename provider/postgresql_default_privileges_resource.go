package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &PostgresqlDefaultPrivilegesResource{}

var validDefaultPrivilegeObjectTypes = []string{"table", "sequence"}

func defaclObjType(objectType string) (string, error) {
	switch objectType {
	case "table":
		return "r", nil
	case "sequence":
		return "S", nil
	default:
		return "", fmt.Errorf("unsupported object_type %q", objectType)
	}
}

func defaultPrivilegesTargetKeyword(objectType string) (string, error) {
	switch objectType {
	case "table":
		return "TABLES", nil
	case "sequence":
		return "SEQUENCES", nil
	default:
		return "", fmt.Errorf("unsupported object_type %q", objectType)
	}
}

func NewPostgresqlDefaultPrivilegesResource() resource.Resource {
	return &PostgresqlDefaultPrivilegesResource{}
}

type PostgresqlDefaultPrivilegesResource struct {
	client *Client
}

type PostgresqlDefaultPrivilegesResourceModel struct {
	ID              types.String `tfsdk:"id"`
	ObjectType      types.String `tfsdk:"object_type"`
	Database        types.String `tfsdk:"database"`
	Schema          types.String `tfsdk:"schema"`
	Role            types.String `tfsdk:"role"`
	Owner           types.String `tfsdk:"owner"`
	Privileges      types.Set    `tfsdk:"privileges"`
	WithGrantOption types.Bool   `tfsdk:"with_grant_option"`
}

func (r *PostgresqlDefaultPrivilegesResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *PostgresqlDefaultPrivilegesResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_default_privileges"
}

func (r *PostgresqlDefaultPrivilegesResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Grants privileges on objects owner creates in schema in the future " +
			"(ALTER DEFAULT PRIVILEGES ... GRANT ...).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"object_type": schema.StringAttribute{
				Required:    true,
				Description: "One of: table, sequence.",
				Validators: []validator.String{
					stringvalidator.OneOf(validDefaultPrivilegeObjectTypes...),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"database": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"schema": schema.StringAttribute{
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				Required:    true,
				Description: "Role the default privileges are granted to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"owner": schema.StringAttribute{
				Required:    true,
				Description: "Role whose future objects (created in schema) get these privileges applied automatically.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"privileges": schema.SetAttribute{
				Required:    true,
				ElementType: types.StringType,
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

func (r *PostgresqlDefaultPrivilegesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PostgresqlDefaultPrivilegesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	privileges := stringSetToGo(plan.Privileges)
	sql, err := defaultPrivilegesGrantSQL(&plan, privileges)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Default Privileges", err.Error())
		return
	}

	tflog.Trace(ctx, "creating postgresql default privileges", map[string]interface{}{
		"role": plan.Role.ValueString(), "owner": plan.Owner.ValueString(), "database": plan.Database.ValueString(),
	})

	err = r.client.WithCatalogLockOnDatabase(ctx, plan.Database.ValueString(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating PostgreSQL Default Privileges", err.Error())
		return
	}

	plan.ID = types.StringValue(defaultPrivilegesID(&plan))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlDefaultPrivilegesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PostgresqlDefaultPrivilegesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	objType, err := defaclObjType(state.ObjectType.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid Default Privileges", err.Error())
		return
	}

	rows, err := r.client.QueryOnDatabase(ctx, state.Database.ValueString(), `
		SELECT priv.privilege_type
		FROM pg_catalog.pg_default_acl d
		JOIN pg_catalog.pg_namespace n ON n.oid = d.defaclnamespace
		JOIN pg_catalog.pg_roles owner_role ON owner_role.oid = d.defaclrole
		CROSS JOIN LATERAL pg_catalog.aclexplode(d.defaclacl) AS priv
		JOIN pg_catalog.pg_roles grantee_role ON grantee_role.oid = priv.grantee
		WHERE owner_role.rolname = $1 AND n.nspname = $2 AND d.defaclobjtype = $3 AND grantee_role.rolname = $4`,
		state.Owner.ValueString(), state.Schema.ValueString(), objType, state.Role.ValueString())
	if err != nil {
		if isDatabaseMissing(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading PostgreSQL Default Privileges", err.Error())
		return
	}
	defer rows.Close()

	var privileges []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			resp.Diagnostics.AddError("Error Reading PostgreSQL Default Privileges", err.Error())
			return
		}
		privileges = append(privileges, p)
	}
	if err := rows.Err(); err != nil {
		resp.Diagnostics.AddError("Error Reading PostgreSQL Default Privileges", err.Error())
		return
	}

	if len(privileges) == 0 {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Privileges = stringSetValue(privileges)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PostgresqlDefaultPrivilegesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state PostgresqlDefaultPrivilegesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	desired := stringSetToGo(plan.Privileges)
	current := stringSetToGo(state.Privileges)
	toGrant, toRevoke := diffStringLists(current, desired)

	tflog.Trace(ctx, "updating postgresql default privileges", map[string]interface{}{
		"role": plan.Role.ValueString(), "owner": plan.Owner.ValueString(), "database": plan.Database.ValueString(),
	})

	err := r.client.WithCatalogLockOnDatabase(ctx, plan.Database.ValueString(), func(ctx context.Context, tx pgx.Tx) error {
		if len(toRevoke) > 0 {
			sql, err := defaultPrivilegesRevokeSQL(&plan, toRevoke)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, sql); err != nil {
				return fmt.Errorf("could not revoke removed default privileges: %w", err)
			}
		}
		if len(toGrant) > 0 || plan.WithGrantOption.ValueBool() != state.WithGrantOption.ValueBool() {
			sql, err := defaultPrivilegesGrantSQL(&plan, desired)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, sql); err != nil {
				return fmt.Errorf("could not grant added/changed default privileges: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating PostgreSQL Default Privileges", err.Error())
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlDefaultPrivilegesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PostgresqlDefaultPrivilegesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	privileges := stringSetToGo(state.Privileges)
	sql, err := defaultPrivilegesRevokeSQL(&state, privileges)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Default Privileges", err.Error())
		return
	}

	tflog.Trace(ctx, "deleting postgresql default privileges", map[string]interface{}{
		"role": state.Role.ValueString(), "owner": state.Owner.ValueString(), "database": state.Database.ValueString(),
	})

	err = r.client.WithCatalogLockOnDatabase(ctx, state.Database.ValueString(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
	if err != nil {
		if isDatabaseMissing(err) {
			tflog.Trace(ctx, "postgresql default privileges' database no longer exists, nothing to revoke", map[string]interface{}{"database": state.Database.ValueString()})
			return
		}
		resp.Diagnostics.AddError("Error Deleting PostgreSQL Default Privileges", err.Error())
		return
	}
}

func defaultPrivilegesGrantSQL(m *PostgresqlDefaultPrivilegesResourceModel, privileges []string) (string, error) {
	targetKeyword, err := defaultPrivilegesTargetKeyword(m.ObjectType.ValueString())
	if err != nil {
		return "", err
	}
	sql := fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA %s GRANT %s ON %s TO %s",
		quoteIdent(m.Owner.ValueString()), quoteIdent(m.Schema.ValueString()),
		strings.Join(privileges, ", "), targetKeyword, quoteIdent(m.Role.ValueString()),
	)
	if m.WithGrantOption.ValueBool() {
		sql += " WITH GRANT OPTION"
	}
	return sql, nil
}

func defaultPrivilegesRevokeSQL(m *PostgresqlDefaultPrivilegesResourceModel, privileges []string) (string, error) {
	targetKeyword, err := defaultPrivilegesTargetKeyword(m.ObjectType.ValueString())
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA %s REVOKE %s ON %s FROM %s",
		quoteIdent(m.Owner.ValueString()), quoteIdent(m.Schema.ValueString()),
		strings.Join(privileges, ", "), targetKeyword, quoteIdent(m.Role.ValueString()),
	), nil
}

func defaultPrivilegesID(m *PostgresqlDefaultPrivilegesResourceModel) string {
	return fmt.Sprintf("%s_%s_%s_%s_%s",
		m.Role.ValueString(), m.Database.ValueString(), m.Schema.ValueString(), m.Owner.ValueString(), m.ObjectType.ValueString())
}
