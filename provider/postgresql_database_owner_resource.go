package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &PostgresqlDatabaseOwnerResource{}

func NewPostgresqlDatabaseOwnerResource() resource.Resource {
	return &PostgresqlDatabaseOwnerResource{}
}

type PostgresqlDatabaseOwnerResource struct {
	client *Client
}

type PostgresqlDatabaseOwnerResourceModel struct {
	Database      types.String `tfsdk:"database"`
	Owner         types.String `tfsdk:"owner"`
	ReassignOwned types.Bool   `tfsdk:"reassign_owned"`
}

func (r *PostgresqlDatabaseOwnerResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *PostgresqlDatabaseOwnerResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database_owner"
}

func (r *PostgresqlDatabaseOwnerResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages only the owner of a PostgreSQL database (ALTER DATABASE ... OWNER TO), " +
			"independent of postgresql_database's own (optional) owner attribute. Destroying this resource " +
			"does NOT revert ownership - there's no well-defined previous owner to revert to - it only stops " +
			"Terraform from managing it.",
		Attributes: map[string]schema.Attribute{
			"database": schema.StringAttribute{
				Required:    true,
				Description: "Name of the database whose owner is managed.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"owner": schema.StringAttribute{
				Required:    true,
				Description: "Role to assign as the database's owner.",
			},
			"reassign_owned": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: reassignOwnedDescription,
			},
		},
	}
}

func (r *PostgresqlDatabaseOwnerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PostgresqlDatabaseOwnerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	warning, err := r.applyOwner(ctx, plan.Database.ValueString(), plan.Owner.ValueString(), plan.ReassignOwned.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Error Setting PostgreSQL Database Owner",
			fmt.Sprintf("Could not set owner of database %s to %s: %s", plan.Database.ValueString(), plan.Owner.ValueString(), err.Error()))
		return
	}
	if warning != "" {
		resp.Diagnostics.AddWarning("Database Objects Not Reassigned", warning)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlDatabaseOwnerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PostgresqlDatabaseOwnerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	warning, err := r.applyOwner(ctx, plan.Database.ValueString(), plan.Owner.ValueString(), plan.ReassignOwned.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Error Setting PostgreSQL Database Owner",
			fmt.Sprintf("Could not set owner of database %s to %s: %s", plan.Database.ValueString(), plan.Owner.ValueString(), err.Error()))
		return
	}
	if warning != "" {
		resp.Diagnostics.AddWarning("Database Objects Not Reassigned", warning)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlDatabaseOwnerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PostgresqlDatabaseOwnerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	database := state.Database.ValueString()
	tflog.Trace(ctx, "reading postgresql database owner", map[string]interface{}{"database": database})

	var owner string
	err := r.client.QueryRow(ctx, `SELECT pg_catalog.pg_get_userbyid(datdba) FROM pg_catalog.pg_database WHERE datname = $1`, database).Scan(&owner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			tflog.Warn(ctx, "postgresql database not found, removing owner resource from state", map[string]interface{}{"database": database})
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading PostgreSQL Database Owner", fmt.Sprintf("Could not read owner of database %s: %s", database, err.Error()))
		return
	}

	state.Owner = types.StringValue(owner)
	if state.ReassignOwned.IsNull() {
		state.ReassignOwned = types.BoolValue(false)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PostgresqlDatabaseOwnerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PostgresqlDatabaseOwnerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tflog.Trace(ctx, "removing postgresql_database_owner from state (ownership left as-is)", map[string]interface{}{
		"database": state.Database.ValueString(),
		"owner":    state.Owner.ValueString(),
	})
}

func (r *PostgresqlDatabaseOwnerResource) applyOwner(ctx context.Context, database, owner string, reassign bool) (string, error) {
	tflog.Trace(ctx, "setting postgresql database owner", map[string]interface{}{"database": database, "owner": owner, "reassign_owned": reassign})
	var warning string
	err := r.client.WithCatalogLockOnDatabase(ctx, r.client.ownerChangeDatabase(database, reassign), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		warning, err = setDatabaseOwner(ctx, tx, database, owner, reassign)
		return err
	})
	return warning, err
}
