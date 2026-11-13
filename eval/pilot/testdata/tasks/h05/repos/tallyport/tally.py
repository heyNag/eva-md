"""tallyport: totals per account from a ledger export."""

from decimal import Decimal


def read_header(text):
    """Return the export's format, 1 or 2, by its header, and its data lines."""
    lines = [line for line in text.splitlines() if line.strip()]
    if not lines:
        return 1, []
    header = lines[0].split(",")
    if header == ["date", "account", "amount"]:
        return 1, lines[1:]
    if header[:3] == ["date", "account", "amount_minor"]:
        return 2, lines[1:]
    raise ValueError("an export with an unknown header")


def totals(text):
    """Return each account's total, in cents."""
    version, lines = read_header(text)
    out = {}
    for line in lines:
        fields = line.split(",")
        account, amount = fields[1], fields[2]
        cents = int(Decimal(amount) * 100)
        out[account] = out.get(account, 0) + cents
    return out
