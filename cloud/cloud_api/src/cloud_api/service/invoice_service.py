from cloud_api.models.api import Invoice, ListInvoiceResponse, QueryInvoiceRequest
from cloud_api.repository.invoice_repository import InvoiceRepository


class InvoiceNotFoundError(Exception):
    def __init__(self, invoice_id: str) -> None:
        super().__init__(f"Invoice '{invoice_id}' not found")
        self.invoice_id = invoice_id


class InvoiceService:
    """Read-only: invoices are a mirror of Stripe's own objects, written
    exclusively by StripeWebhookService."""

    def __init__(self, invoice_repo: InvoiceRepository) -> None:
        self.invoice_repo = invoice_repo

    async def get_invoices(self, req: QueryInvoiceRequest) -> ListInvoiceResponse:
        items = await self.invoice_repo.query_invoices(req)
        return ListInvoiceResponse(items=items, total=len(items))

    async def get_invoice(self, invoice_id: str) -> Invoice:
        invoice = await self.invoice_repo.get_invoice(invoice_id)
        if invoice is None:
            raise InvoiceNotFoundError(invoice_id)
        return invoice
