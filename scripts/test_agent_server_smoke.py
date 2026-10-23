import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("smoke", Path(__file__).with_name("agent-server-smoke.py"))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class SourceTimestampTests(unittest.TestCase):
    def test_go_nanoseconds_and_stored_milliseconds_match_without_rounding(self):
        self.assertEqual(smoke.timestamp_millis("2026-10-23T16:02:22.999999999Z"),
                         smoke.timestamp_millis("2026-10-23T16:02:22.999Z"))
        self.assertLess(smoke.timestamp("2026-10-23T16:02:22.999999999Z"),
                        smoke.timestamp("2026-10-23T16:02:23Z"))

    def test_fraction_lengths_and_timezone_offset(self):
        for fraction in ["1", "10", "100", "1000", "10000", "100000", "1000000", "10000000", "100000000"]:
            self.assertEqual(smoke.timestamp_millis("2026-10-23T16:02:22." + fraction + "Z"),
                             smoke.timestamp_millis("2026-10-24T00:02:22.100+08:00"))

    def test_missing_timezone_and_invalid_source_are_rejected(self):
        for value in [None, "", "2026-10-23T16:02:22", "2026-10-23T16:02:22.1234567890Z"]:
            with self.assertRaises(AssertionError):
                smoke.timestamp(value)


if __name__ == "__main__":
    unittest.main()
