"""Format-valid but FAKE PII/secret generators for the audit test-data corpus.

Every value is clearly fake yet passes the format/checksum a naive detector
would apply. Used for authoring the corpus and (optionally) for variety.
Stdlib only.
"""
import random
import string

_ID_WEIGHTS = [7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2]
_ID_CHECK = ["1", "0", "X", "9", "8", "7", "6", "5", "4", "3", "2"]
# A few real region codes so IDs look plausible.
_REGIONS = ["110105", "310115", "440305", "510107", "330106", "420106"]


def china_id_checksum(first17: str) -> str:
    total = sum(int(first17[i]) * _ID_WEIGHTS[i] for i in range(17))
    return _ID_CHECK[total % 11]


def valid_china_id(id18: str) -> bool:
    if len(id18) != 18 or not id18[:17].isdigit():
        return False
    return china_id_checksum(id18[:17]).upper() == id18[17].upper()


def china_id(rng=None) -> str:
    rng = rng or random.Random()
    region = rng.choice(_REGIONS)
    year = rng.randint(1960, 2004)
    month = rng.randint(1, 12)
    day = rng.randint(1, 28)
    seq = rng.randint(1, 999)
    first17 = f"{region}{year:04d}{month:02d}{day:02d}{seq:03d}"
    return first17 + china_id_checksum(first17)


def _luhn_total(digits: str) -> int:
    total = 0
    # rightmost digit has parity 0; double every second from the right
    for i, ch in enumerate(reversed(digits)):
        d = int(ch)
        if i % 2 == 1:
            d *= 2
            if d > 9:
                d -= 9
        total += d
    return total


def luhn_check_digit(prefix: str) -> str:
    # check digit makes (prefix + check) Luhn-valid; prefix is the number
    # WITHOUT the check digit, so the check digit sits at parity 0.
    t = _luhn_total(prefix + "0")
    return str((10 - t % 10) % 10)


def valid_luhn(number: str) -> bool:
    digits = number.replace(" ", "")
    if not digits.isdigit():
        return False
    return _luhn_total(digits) % 10 == 0


def unionpay_card(rng=None) -> str:
    rng = rng or random.Random()
    body = "62" + "".join(str(rng.randint(0, 9)) for _ in range(13))
    return body + luhn_check_digit(body)


def china_mobile(rng=None) -> str:
    rng = rng or random.Random()
    return "1" + str(rng.randint(3, 9)) + "".join(str(rng.randint(0, 9)) for _ in range(9))


def _rand_str(rng, alphabet, n):
    return "".join(rng.choice(alphabet) for _ in range(n))


def aws_access_key(rng=None) -> str:
    rng = rng or random.Random()
    return "AKIA" + _rand_str(rng, string.ascii_uppercase + string.digits, 16)


def github_pat(rng=None) -> str:
    rng = rng or random.Random()
    return "ghp_" + _rand_str(rng, string.ascii_letters + string.digits, 36)


def jwt(rng=None) -> str:
    rng = rng or random.Random()
    alphabet = string.ascii_letters + string.digits + "-_"
    header = "eyJ" + _rand_str(rng, alphabet, 16)
    payload = _rand_str(rng, alphabet, 40)
    sig = _rand_str(rng, alphabet, 32)
    return f"{header}.{payload}.{sig}"


def pem_private_key() -> str:
    return (
        "-----BEGIN RSA PRIVATE KEY-----\n"
        "MIIEpAIBAAKCAQEA1xFAKEfakeFAKEexampleKEYdoNOTuse0000000000000000\n"
        "0000000000000000000000000000000000000000000000000000000000000000\n"
        "-----END RSA PRIVATE KEY-----"
    )
