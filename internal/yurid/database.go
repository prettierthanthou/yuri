package yurid

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"codeberg.org/lewdest/yuri"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
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
	db *sql.DB
}

func NewDatabase(conf DatabaseConfig) (*database, error) {
	db, err := sql.Open(string(conf.Type), conf.DSN)
	if err != nil {
		return nil, err
	}

	database := &database{db: db}
	if err := database.ensureSchema(); err != nil {
		return nil, err
	}

	return database, nil
}

func (d *database) ensureSchema() error {
	_, err := d.db.Exec(`
	create table if not exists "invoice" (
		id TEXT NOT NULL PRIMARY KEY,
		chain TEXT NOT NULL,
		address TEXT NOT NULL,
		amount_owed TEXT NOT NULL,
		amount_paid TEXT NOT NULL,
		token TEXT NOT NULL,
		metadata TEXT NOT NULL,
		expires_at TIMESTAMP,

		UNIQUE(chain, address)
	)
	`)
	return err
}

func (d *database) GetInvoiceByID(ctx context.Context, id string) (*yuri.Invoice, error) {
	row := d.db.QueryRowContext(ctx, `
		select chain, address, amount_owed, amount_paid, token, metadata, expires_at
		from invoice
		where id = ?
	`, id)

	var (
		chainStr  string
		address   string
		owedStr   string
		paidStr   string
		tokenStr  string
		metaStr   string
		expiresAt time.Time
	)

	if err := row.Scan(
		&chainStr,
		&address,
		&owedStr,
		&paidStr,
		&tokenStr,
		&metaStr,
		&expiresAt,
	); err != nil {
		return nil, err
	}

	owed := new(big.Int)
	owed.SetString(owedStr, 10)

	paid := new(big.Int)
	paid.SetString(paidStr, 10)

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
		Metadata:   metadata,
	}, nil
}

// GetActiveInvoices implements [yuri.Storage].
func (d *database) GetActiveInvoices(ctx context.Context, chain yuri.Chain) ([]yuri.Invoice, error) {
	rows, err := d.db.QueryContext(ctx, `
		select id, chain, address, amount_owed, amount_paid, token, metadata, expires_at
		from "invoice"
		where chain = ?
		  and expires_at > CURRENT_TIMESTAMP
	`, chain)
	if err != nil {
		return nil, fmt.Errorf("querying active invoices failed: %+v", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var collectedInvoices []yuri.Invoice

	for rows.Next() {
		var (
			id            string
			chainStr      string
			address       string
			amountOwedStr string
			amountPaidStr string
			tokenStr      string
			metadataStr   string
			expiresAt     time.Time
		)

		if err := rows.Scan(
			&id,
			&chainStr,
			&address,
			&amountOwedStr,
			&amountPaidStr,
			&tokenStr,
			&metadataStr,
			&expiresAt,
		); err != nil {
			return nil, err
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

	_, err = d.db.ExecContext(ctx, `
		insert into "invoice"
			(id, chain, address, amount_owed, amount_paid, token, metadata, expires_at)
		values
			(?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(chain, address) do update set
			amount_owed = excluded.amount_owed,
			amount_paid = excluded.amount_paid,
			token = excluded.token,
			metadata = excluded.metadata,
			expires_at = excluded.expires_at
	`,
		id,
		inv.Chain,
		inv.Address,
		inv.AmountOwed.String(),
		inv.AmountPaid.String(),
		tokenJSON,
		metaJSON,
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

	stmt, err := tx.PrepareContext(ctx, `
		insert into "invoice"
			(id, chain, address, amount_owed, amount_paid, token, metadata)
		values
			(?, ?, ?, ?, ?, ?, ?)
		on conflict(chain, address) do update set
			amount_owed = excluded.amount_owed,
			amount_paid = excluded.amount_paid,
			token = excluded.token,
			metadata = excluded.metadata
	`)
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
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
