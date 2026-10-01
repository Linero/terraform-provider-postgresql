package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*PostgresqlProvider)(nil)

type PostgresqlProvider struct{}

func New() func() provider.Provider {
	return func() provider.Provider {
		return &PostgresqlProvider{}
	}
}

type PostgresqlProviderModel struct {
	Host           types.String `tfsdk:"host"`
	Port           types.Int64  `tfsdk:"port"`
	Username       types.String `tfsdk:"username"`
	Password       types.String `tfsdk:"password"`
	Database       types.String `tfsdk:"database"`
	Sslmode        types.String `tfsdk:"sslmode"`
	ConnectTimeout types.Int64  `tfsdk:"connect_timeout"`
}

func (p *PostgresqlProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "postgresql"
}

func (p *PostgresqlProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Required:    true,
				Description: "PostgreSQL server host.",
			},
			"port": schema.Int64Attribute{
				Optional:    true,
				Description: "PostgreSQL server port. Defaults to 5432.",
			},
			"username": schema.StringAttribute{
				Required:    true,
				Description: "Username used to authenticate the admin connection.",
			},
			"password": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Password used to authenticate the admin connection.",
			},
			"database": schema.StringAttribute{
				Optional:    true,
				Description: "Database the provider connects to in order to issue role/database/grant DDL. Defaults to \"postgres\".",
			},
			"sslmode": schema.StringAttribute{
				Optional:    true,
				Description: "SSL mode for the connection (disable, allow, prefer, require, verify-ca, verify-full). Defaults to \"prefer\".",
			},
			"connect_timeout": schema.Int64Attribute{
				Optional:    true,
				Description: "Maximum wait, in seconds, for a connection to be established. Defaults to 15.",
			},
		},
	}
}

func (p *PostgresqlProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data PostgresqlProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	port := int64(5432)
	if !data.Port.IsNull() {
		port = data.Port.ValueInt64()
	}
	database := "postgres"
	if !data.Database.IsNull() && data.Database.ValueString() != "" {
		database = data.Database.ValueString()
	}
	sslmode := "prefer"
	if !data.Sslmode.IsNull() && data.Sslmode.ValueString() != "" {
		sslmode = data.Sslmode.ValueString()
	}
	connectTimeout := int64(15)
	if !data.ConnectTimeout.IsNull() {
		connectTimeout = data.ConnectTimeout.ValueInt64()
	}

	cfg := connConfig{
		Host:           data.Host.ValueString(),
		Port:           port,
		Username:       data.Username.ValueString(),
		Password:       data.Password.ValueString(),
		Sslmode:        sslmode,
		ConnectTimeout: connectTimeout,
	}

	client, err := NewClient(ctx, cfg, database)
	if err != nil {
		resp.Diagnostics.AddError("Failed to configure PostgreSQL client", err.Error())
		return
	}

	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *PostgresqlProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func (p *PostgresqlProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPostgresqlRoleResource,
		NewPostgresqlDatabaseResource,
		NewPostgresqlDatabaseOwnerResource,
		NewPostgresqlGrantResource,
		NewPostgresqlDefaultPrivilegesResource,
		NewPostgresqlGrantRoleResource,
		NewPostgresqlSessionRoleResource,
	}
}
