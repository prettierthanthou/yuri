package yurid

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"codeberg.org/lewdest/yuri"
	"github.com/google/uuid"
)

const yuridInvoiceUUIDMetaId = "yurid-uuid"
const yuridInvoiceExpireyMetaID = "yurid-expirey"
const yuridInvoiceFiatMetaID = "yurid-fiat-hist"

type DatabaseType string

const (
	DatabaseTypeSqlite   DatabaseType = "sqlite"
	DatabaseTypePostgres DatabaseType = "postgresql"
	DatabaseTypeMysql    DatabaseType = "mysql"
)

type DatabaseConfig struct {
	Type DatabaseType
	DSN  string
}

var _ yuri.Storage = (*database)(nil)
var _ Database = (*database)(nil)

type Database interface {
	yuri.Storage
	GetInvoiceByID(ctx context.Context, id string) (*yuri.Invoice, error)
	NewInvoiceWithExpirey(ctx context.Context, inv yuri.Invoice, expiresAt time.Time) (uuid.UUID, error)
	ensureSchema() error
}

type database struct {
	conf DatabaseConfig
	db   *sql.DB
}

func NewDatabase(conf DatabaseConfig) (*database, error) {
	typ := string(conf.Type)
	if conf.Type == DatabaseTypePostgres {
		typ = "pgx"
	}

	db, err := sql.Open(typ, conf.DSN)
	if err != nil {
		return nil, err
	}

	database := &database{conf: conf, db: db}
	if err := database.ensureSchema(); err != nil {
		return nil, err
	}

	return database, nil
}

// quoteIdent quotes an identifier using the configured dialect.
func (d *database) quoteIdent(name string) string {
	if d.conf.Type == DatabaseTypeMysql {
		return "`" + name + "`"
	}
	return `"` + name + `"`
}

// rewrite adapts double-quoted identifiers and `?` placeholders to the dialect.
func (d *database) rewrite(stmt string) string {
	if d.conf.Type == DatabaseTypePostgres {
		var b strings.Builder
		arg := 0
		for i := 0; i < len(stmt); i++ {
			if stmt[i] == '?' {
				arg++
				fmt.Fprintf(&b, "$%d", arg)
			} else {
				b.WriteByte(stmt[i])
			}
		}
		stmt = b.String()
	}

	if d.conf.Type == DatabaseTypeMysql {
		stmt = strings.ReplaceAll(stmt, `"`, "`")
	}

	return stmt
}

// upsertClause renders the conflict-resolution clause for the dialect.
func (d *database) upsertClause(uniqueCols, updateCols []string) string {
	assignments := make([]string, len(updateCols))
	for i, col := range updateCols {
		quoted := d.quoteIdent(col)
		if d.conf.Type == DatabaseTypeMysql {
			assignments[i] = fmt.Sprintf("%s = values(%s)", quoted, quoted)
		} else {
			assignments[i] = fmt.Sprintf("%s = excluded.%s", quoted, quoted)
		}
	}

	joined := strings.Join(assignments, ", ")
	if d.conf.Type == DatabaseTypeMysql {
		return "on duplicate key update " + joined
	}

	quotedUnique := make([]string, len(uniqueCols))
	for i, col := range uniqueCols {
		quotedUnique[i] = d.quoteIdent(col)
	}

	return fmt.Sprintf(
		"on conflict (%s) do update set %s",
		strings.Join(quotedUnique, ", "),
		joined,
	)
}

func (d *database) Close() error {
	return d.db.Close()
}

func (d *database) ensureSchema() error {
	_, err := d.db.Exec(d.rewrite(`
	create table if not exists "invoice" (
		id TEXT NOT NULL PRIMARY KEY,
		chain TEXT NOT NULL,
		address TEXT NOT NULL,
		amount_owed TEXT NOT NULL,
		amount_paid TEXT NOT NULL,
		token TEXT NOT NULL,
		metadata TEXT NOT NULL,
		pending BOOLEAN NOT NULL DEFAULT FALSE,
		expires_at TIMESTAMP,

		UNIQUE(chain, address)
	)
	`))
	if err != nil {
		return err
	}

	// migrate databases created before pending was tracked
	if _, err := d.db.Exec(d.rewrite(`
		alter table "invoice" add column pending BOOLEAN NOT NULL DEFAULT FALSE
	`)); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
			return fmt.Errorf("migrating pending column: %w", err)
		}
	}

	return nil
}

func (d *database) GetInvoiceByID(ctx context.Context, id string) (*yuri.Invoice, error) {
	row := d.db.QueryRowContext(ctx, d.rewrite(`
		select chain, address, amount_owed, amount_paid, token, metadata, pending, expires_at
		from "invoice"
		where id = ?
	`), id)

	var (
		chainStr  string
		address   string
		owedStr   string
		paidStr   string
		tokenStr  string
		metaStr   string
		pending   bool
		expiresAt sql.NullTime
	)

	if err := row.Scan(
		&chainStr,
		&address,
		&owedStr,
		&paidStr,
		&tokenStr,
		&metaStr,
		&pending,
		&expiresAt,
	); err != nil {
		return nil, err
	}

	owed := new(big.Int)
	if _, ok := owed.SetString(owedStr, 10); !ok {
		return nil, fmt.Errorf("failed to SetString for owedStr")
	}

	paid := new(big.Int)
	if _, ok := paid.SetString(paidStr, 10); !ok {
		return nil, fmt.Errorf("failed to SetString for paidStr")
	}

	var token yuri.Token
	if err := json.Unmarshal([]byte(tokenStr), &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %+v", err)
	}

	var metadata map[string]any
	if err := json.Unmarshal([]byte(metaStr), &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %+v", err)
	}

	return &yuri.Invoice{
		Chain:      yuri.Chain(chainStr),
		Address:    address,
		AmountOwed: owed,
		AmountPaid: paid,
		Token:      token,
		Pending:    pending,
		Metadata:   metadata,
	}, nil
}

// GetActiveInvoices implements [yuri.Storage].
func (d *database) GetActiveInvoices(ctx context.Context, chain yuri.Chain) ([]yuri.Invoice, error) {
	rows, err := d.db.QueryContext(ctx, d.rewrite(`
		select id, chain, address, amount_owed, amount_paid, token, metadata, pending, expires_at
		from "invoice"
		where chain = ?
	`), chain)
	if err != nil {
		return nil, fmt.Errorf("querying active invoices failed: %+v", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var collectedInvoices []yuri.Invoice
	now := time.Now()

	for rows.Next() {
	var (
		id            string
		chainStr      string
		address       string
		amountOwedStr string
		amountPaidStr string
		tokenStr      string
		metadataStr   string
		pending       bool
		expiresAt     sql.NullTime
	)

	if err := rows.Scan(
		&id,
		&chainStr,
		&address,
		&amountOwedStr,
		&amountPaidStr,
		&tokenStr,
		&metadataStr,
		&pending,
		&expiresAt,
	); err != nil {
		return nil, err
	}

		// NULL expiry means the invoice never expires
		if expiresAt.Valid && expiresAt.Time.Before(now) {
			continue
		}

		amountOwed := new(big.Int)
		if _, ok := amountOwed.SetString(amountOwedStr, 10); !ok {
			return nil, fmt.Errorf("failed to parse amount owed")
		}

		amountPaid := new(big.Int)
		if _, ok := amountPaid.SetString(amountPaidStr, 10); !ok {
			return nil, fmt.Errorf("failed to parse amount paid")
		}

		var token yuri.Token
		if err := json.Unmarshal([]byte(tokenStr), &token); err != nil {
			return nil, fmt.Errorf("failed to unmarshal token: %+v", err)
		}

		var metadata map[string]any
		if err := json.Unmarshal([]byte(metadataStr), &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %+v", err)
		}

		metadata[yuridInvoiceUUIDMetaId] = id
		collectedInvoices = append(collectedInvoices, yuri.Invoice{
			Chain:      yuri.Chain(chainStr),
			Address:    address,
			AmountOwed: amountOwed,
			AmountPaid: amountPaid,
			Token:      token,
			Pending:    pending,
			Metadata:   metadata,
		})
	}

	return collectedInvoices, rows.Err()
}

func (d *database) NewInvoiceWithExpirey(ctx context.Context, inv yuri.Invoice, expiresAt time.Time) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("uuid: %w", err)
	}

	inv.Metadata[yuridInvoiceUUIDMetaId] = id
	metaJSON, err := json.Marshal(inv.Metadata)
	if err != nil {
		return uuid.Nil, fmt.Errorf("metadata: %w", err)
	}

	tokenJSON, err := json.Marshal(inv.Token)
	if err != nil {
		return uuid.Nil, fmt.Errorf("token: %w", err)
	}

	conflict := d.upsertClause(
		[]string{"chain", "address"},
		[]string{"amount_owed", "amount_paid", "token", "metadata", "pending", "expires_at"},
	)

	_, err = d.db.ExecContext(ctx, d.rewrite(fmt.Sprintf(`
		insert into "invoice"
			(id, chain, address, amount_owed, amount_paid, token, metadata, pending, expires_at)
		values
			(?, ?, ?, ?, ?, ?, ?, ?, ?)
		%s
	`, conflict)),
		id,
		inv.Chain,
		inv.Address,
		inv.AmountOwed.String(),
		inv.AmountPaid.String(),
		tokenJSON,
		metaJSON,
		inv.Pending,
		expiresAt,
	)

	return id, err
}

// NewInvoice implements [yuri.Storage].
func (d *database) NewInvoice(ctx context.Context, inv yuri.Invoice) error {
	_, ok := inv.Metadata[yuridInvoiceExpireyMetaID]
	if !ok {
		_, err := d.NewInvoiceWithExpirey(ctx, inv, time.Now().Add(30*time.Minute))
		return err
	}

	castedExpirey, ok := inv.Metadata[yuridInvoiceExpireyMetaID].(time.Time)
	if !ok {
		return errors.New("found yuridInvoiceExpireyMetaID but it was malformed?")
	}

	_, err := d.NewInvoiceWithExpirey(ctx, inv, castedExpirey)
	return err
}

// UpdateInvoices implements [yuri.Storage].
func (d *database) UpdateInvoices(ctx context.Context, invoices []yuri.Invoice) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	conflict := d.upsertClause(
		[]string{"chain", "address"},
		[]string{"amount_owed", "amount_paid", "token", "metadata", "pending"},
	)

	stmt, err := tx.PrepareContext(ctx, d.rewrite(fmt.Sprintf(`
		insert into "invoice"
			(id, chain, address, amount_owed, amount_paid, token, metadata, pending)
		values
			(?, ?, ?, ?, ?, ?, ?, ?)
		%s
	`, conflict)))
	if err != nil {
		return err
	}
	defer func() {
		_ = stmt.Close()
	}()

	for _, inv := range invoices {
		invoiceId, ok := inv.Metadata[yuridInvoiceUUIDMetaId]
		if !ok {
			slog.Warn("invoice found without required invoice UUID metadata for yurid. this might have been created outside yurid, or something has gone wrong.", "invoice", inv)
			continue
		}

		metaJSON, err := json.Marshal(inv.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}

		tokenJSON, err := json.Marshal(inv.Token)
		if err != nil {
			return fmt.Errorf("marshal token: %w", err)
		}

		_, err = stmt.ExecContext(ctx,
			invoiceId,
			inv.Chain,
			inv.Address,
			inv.AmountOwed.String(),
			inv.AmountPaid.String(),
			tokenJSON,
			metaJSON,
			inv.Pending,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
