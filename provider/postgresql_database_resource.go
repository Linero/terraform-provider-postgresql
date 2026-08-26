package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &PostgresqlDatabaseResource{}

func NewPostgresqlDatabaseResource() resource.Resource {
	return &PostgresqlDatabaseResource{}
}

type PostgresqlDatabaseResource struct {
	client *Client
}

type PostgresqlDatabaseResourceModel struct {
	Name            types.String `tfsdk:"name"`
	Owner           types.String `tfsdk:"owner"`
	Encoding        types.String `tfsdk:"encoding"`
	LcCollate       types.String `tfsdk:"lc_collate"`
	LcCtype         types.String `tfsdk:"lc_ctype"`
	Template        types.String `tfsdk:"template"`
	ConnectionLimit types.Int64  `tfsdk:"connection_limit"`
}

func (r *PostgresqlDatabaseResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *PostgresqlDatabaseResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (r *PostgresqlDatabaseResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A PostgreSQL database (CREATE DATABASE).",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Database name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"owner": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Owning role. When set, applied via ALTER DATABASE ... OWNER TO right after " +
					"creation and kept in sync on every update. When left unset, ownership defaults to " +
					"whichever role this provider connects as, and this attribute simply tracks the database's " +
					"actual current owner (e.g. as changed by a separate postgresql_database_owner resource) " +
					"without trying to manage it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"encoding": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("UTF8"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"lc_collate": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("en_US.UTF-8"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"lc_ctype": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("en_US.UTF-8"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"template": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("template0"),
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"connection_limit": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(-1),
				Description: "-1 means no limit.",
			},
		},
	}
}

func (r *PostgresqlDatabaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

func (r *PostgresqlDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PostgresqlDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	tflog.Trace(ctx, "creating postgresql database", map[string]interface{}{"database": name})

	createSQL := fmt.Sprintf(
		"CREATE DATABASE %s WITH TEMPLATE %s ENCODING %s LC_COLLATE %s LC_CTYPE %s CONNECTION LIMIT %d",
		quoteIdent(name),
		quoteIdent(plan.Template.ValueString()),
		quoteLiteral(plan.Encoding.ValueString()),
		quoteLiteral(plan.LcCollate.ValueString()),
		quoteLiteral(plan.LcCtype.ValueString()),
		plan.ConnectionLimit.ValueInt64(),
	)

	adopted := false
	err := r.client.WithCatalogLockNoTx(ctx, func(ctx context.Context, conn *pgxpool.Conn) error {
		if _, err := conn.Exec(ctx, createSQL); err != nil {
			if !isPgErrorCode(err, pgErrDuplicateDatabase) {
				return fmt.Errorf("could not create database: %w", err)
			}

			adopted = true
		}
		if !plan.Owner.IsNull() && !plan.Owner.IsUnknown() {
			ownerSQL := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", quoteIdent(name), quoteIdent(plan.Owner.ValueString()))
			if _, err := conn.Exec(ctx, ownerSQL); err != nil {
				return fmt.Errorf("could not set database owner: %w", err)
			}
		}
		if adopted {
			connLimitSQL := fmt.Sprintf("ALTER DATABASE %s CONNECTION LIMIT %d", quoteIdent(name), plan.ConnectionLimit.ValueInt64())
			if _, err := conn.Exec(ctx, connLimitSQL); err != nil {
				return fmt.Errorf("database %q already existed and its connection limit could not be updated to match configuration: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating PostgreSQL Database", fmt.Sprintf("Could not create database %s: %s", name, err.Error()))
		return
	}

	attrs, found, err := r.readDatabaseAttributes(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating PostgreSQL Database", fmt.Sprintf("Database %s was created but its attributes could not be read back: %s", name, err.Error()))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Error Creating PostgreSQL Database", fmt.Sprintf("Database %s was reported as created but is missing on read-back", name))
		return
	}

	if adopted {
		var mismatches []string
		if plan.Encoding.ValueString() != attrs.encoding {
			mismatches = append(mismatches, fmt.Sprintf("encoding (configured %q, actual %q)", plan.Encoding.ValueString(), attrs.encoding))
		}
		if plan.LcCollate.ValueString() != attrs.lcCollate {
			mismatches = append(mismatches, fmt.Sprintf("lc_collate (configured %q, actual %q)", plan.LcCollate.ValueString(), attrs.lcCollate))
		}
		if plan.LcCtype.ValueString() != attrs.lcCtype {
			mismatches = append(mismatches, fmt.Sprintf("lc_ctype (configured %q, actual %q)", plan.LcCtype.ValueString(), attrs.lcCtype))
		}
		if len(mismatches) > 0 {
			resp.Diagnostics.AddError(
				"Error Creating PostgreSQL Database",
				fmt.Sprintf(
					"Database %s already exists and was adopted, but its creation-time attributes differ from the configuration and cannot be changed without dropping and recreating the database: %s. "+
						"Update the configuration to match the existing database, or drop the database and let Terraform recreate it.",
					name, strings.Join(mismatches, ", "),
				),
			)
			return
		}
	}

	attrs.applyTo(&plan)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PostgresqlDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	tflog.Trace(ctx, "reading postgresql database", map[string]interface{}{"database": name})

	attrs, found, err := r.readDatabaseAttributes(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading PostgreSQL Database", fmt.Sprintf("Could not read database %s: %s", name, err.Error()))
		return
	}
	if !found {
		tflog.Warn(ctx, "postgresql database not found, removing from state", map[string]interface{}{"database": name})
		resp.State.RemoveResource(ctx)
		return
	}
	attrs.applyTo(&state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PostgresqlDatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state PostgresqlDatabaseResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	tflog.Trace(ctx, "updating postgresql database", map[string]interface{}{"database": name})

	err := r.client.WithCatalogLock(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if !plan.Owner.IsNull() && !plan.Owner.IsUnknown() && plan.Owner.ValueString() != state.Owner.ValueString() {
			sql := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", quoteIdent(name), quoteIdent(plan.Owner.ValueString()))
			if _, err := tx.Exec(ctx, sql); err != nil {
				return fmt.Errorf("could not set database owner: %w", err)
			}
		}
		if plan.ConnectionLimit.ValueInt64() != state.ConnectionLimit.ValueInt64() {
			sql := fmt.Sprintf("ALTER DATABASE %s CONNECTION LIMIT %d", quoteIdent(name), plan.ConnectionLimit.ValueInt64())
			if _, err := tx.Exec(ctx, sql); err != nil {
				return fmt.Errorf("could not set database connection limit: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating PostgreSQL Database", fmt.Sprintf("Could not update database %s: %s", name, err.Error()))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PostgresqlDatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PostgresqlDatabaseResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	tflog.Trace(ctx, "deleting postgresql database", map[string]interface{}{"database": name})

	r.client.ClosePool(name)

	err := r.client.WithCatalogLockNoTx(ctx, func(ctx context.Context, conn *pgxpool.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("DROP DATABASE %s", quoteIdent(name)))
		return err
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Deleting PostgreSQL Database", fmt.Sprintf("Could not delete database %s: %s", name, err.Error()))
		return
	}
}

type databaseAttributes struct {
	owner, encoding, lcCollate, lcCtype string
	connLimit                           int64
}

func (a databaseAttributes) applyTo(m *PostgresqlDatabaseResourceModel) {
	m.Owner = types.StringValue(a.owner)
	m.Encoding = types.StringValue(a.encoding)
	m.LcCollate = types.StringValue(a.lcCollate)
	m.LcCtype = types.StringValue(a.lcCtype)
	m.ConnectionLimit = types.Int64Value(a.connLimit)
}

func (r *PostgresqlDatabaseResource) readDatabaseAttributes(ctx context.Context, name string) (attrs databaseAttributes, found bool, err error) {
	row := r.client.QueryRow(ctx, `
		SELECT pg_catalog.pg_get_userbyid(d.datdba), pg_catalog.pg_encoding_to_char(d.encoding),
		       d.datcollate, d.datctype, d.datconnlimit
		FROM pg_catalog.pg_database d WHERE d.datname = $1`, name)

	if err := row.Scan(&attrs.owner, &attrs.encoding, &attrs.lcCollate, &attrs.lcCtype, &attrs.connLimit); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return databaseAttributes{}, false, nil
		}
		return databaseAttributes{}, false, err
	}
	return attrs, true, nil
}
