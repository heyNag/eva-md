"""ledgerhub's export: the entries of a ledger as CSV text."""

HEADER = "date,account,amount_minor,kind"


def export(entries):
    """Return the CSV text of entries, in format 2.

    Each entry is a dict of date (YYYY-MM-DD), account, cents (a whole
    number) and refund (True for money given back). Format 2 writes the
    amount in cents and names each row's kind: sale, or reversal for a
    refund. Format 1, before it, wrote date,account,amount, the amount in
    whole units with two decimals, and wrote a refund as a sale.
    """
    lines = [HEADER]
    for e in entries:
        kind = "reversal" if e["refund"] else "sale"
        lines.append("%s,%s,%d,%s" % (e["date"], e["account"], e["cents"], kind))
    return "\n".join(lines) + "\n"
