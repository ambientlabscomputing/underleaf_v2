from datetime import UTC, datetime

from sqlalchemy import select
from sqlalchemy.dialects.postgresql import insert as pg_insert

from cloud_api.models.api import (
    Invoice,
    QueryInvoiceRequest,
    UpsertInvoiceRequest,
)
from cloud_api.models.sql import SQLInvoice
from cloud_api.repository.base_repository import BaseRepository


class InvoiceRepository(BaseRepository):
    async def upsert_invoice(self, req: UpsertInvoiceRequest) -> Invoice:
        """Invoices are a read-only mirror of Stripe's own objects -- each
        webhook delivery for an invoice just re-states its current fields,
        keyed by stripe_invoice_id, rather than distinguishing create vs
        update at the call site."""
        now = datetime.now(UTC)
        async with self.get_session() as session:
            values = req.model_dump()
            stmt = pg_insert(SQLInvoice).values(
                **values, created_at=now, updated_at=now
            )
            stmt = stmt.on_conflict_do_update(
                index_elements=[SQLInvoice.stripe_invoice_id],
                set_={**values, "updated_at": now},
            ).returning(SQLInvoice)
            result = await session.execute(stmt)
            await session.commit()
            invoice = result.scalar_one()
            return Invoice.model_validate(invoice)

    async def get_invoice(self, invoice_id: str) -> Invoice | None:
        async with self.get_session() as session:
            invoice = await session.get(SQLInvoice, invoice_id)
            if invoice is None:
                return None
            return Invoice.model_validate(invoice)

    async def query_invoices(self, req: QueryInvoiceRequest) -> list[Invoice]:
        async with self.get_session() as session:
            query = select(SQLInvoice)
            if req.billing_account_id is not None:
                query = query.filter(
                    SQLInvoice.billing_account_id == req.billing_account_id
                )
            query = query.order_by(SQLInvoice.created_at.desc())
            result = await session.scalars(query)
            invoices = result.all()
            return [Invoice.model_validate(invoice) for invoice in invoices]
