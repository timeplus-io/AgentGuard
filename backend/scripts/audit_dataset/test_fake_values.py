import random
import unittest

import fake_values as fv


class TestChecksums(unittest.TestCase):
    def test_known_valid_china_id(self):
        # Precomputed valid ID (region 110101, DOB 19900307, seq 346, check 6).
        self.assertTrue(fv.valid_china_id("110101199003073466"))

    def test_china_id_checksum_value(self):
        self.assertEqual(fv.china_id_checksum("11010119900307346"), "6")

    def test_rejects_bad_china_id_check(self):
        self.assertFalse(fv.valid_china_id("110101199003073467"))

    def test_rejects_wrong_length(self):
        self.assertFalse(fv.valid_china_id("11010119900307346"))

    def test_known_valid_luhn(self):
        self.assertTrue(fv.valid_luhn("4111111111111111"))
        self.assertTrue(fv.valid_luhn("4111 1111 1111 1111"))

    def test_rejects_bad_luhn(self):
        self.assertFalse(fv.valid_luhn("4111111111111112"))


class TestGenerators(unittest.TestCase):
    def setUp(self):
        self.rng = random.Random(42)

    def test_generated_china_id_is_valid(self):
        for _ in range(50):
            self.assertTrue(fv.valid_china_id(fv.china_id(self.rng)))

    def test_generated_card_is_luhn_valid(self):
        for _ in range(50):
            card = fv.unionpay_card(self.rng)
            self.assertTrue(card.startswith("62"))
            self.assertTrue(fv.valid_luhn(card))

    def test_mobile_format(self):
        import re
        for _ in range(50):
            self.assertRegex(fv.china_mobile(self.rng), r"^1[3-9]\d{9}$")

    def test_secret_formats(self):
        import re
        self.assertRegex(fv.aws_access_key(self.rng), r"^AKIA[0-9A-Z]{16}$")
        self.assertRegex(fv.github_pat(self.rng), r"^ghp_[A-Za-z0-9]{36}$")
        self.assertRegex(fv.jwt(self.rng), r"^eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$")
        self.assertIn("BEGIN RSA PRIVATE KEY", fv.pem_private_key())


if __name__ == "__main__":
    unittest.main()
