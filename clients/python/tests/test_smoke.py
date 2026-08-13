"""Smoke test for the generated gooey.control.v1 Python client.

unittest on purpose (no pytest dependency); pytest runs these too.

    PYTHONPATH=clients/python python3 -m unittest discover -s clients/python/tests
"""

import unittest

from google.protobuf import duration_pb2

from gooey.control.v1 import control_pb2, session_pb2, types_pb2
from gooey.control.v1 import control_pb2_grpc, session_pb2_grpc


class TypedValueRoundTrip(unittest.TestCase):
    def round_trip(self, tv):
        parsed = types_pb2.TypedValue()
        parsed.ParseFromString(tv.SerializeToString())
        self.assertEqual(parsed, tv)
        return parsed

    def test_every_propkinds_row(self):
        cases = [
            types_pb2.TypedValue(string_value="hi"),
            types_pb2.TypedValue(int_value=-42),
            types_pb2.TypedValue(bool_value=True),
            types_pb2.TypedValue(float_value=1.5),
            types_pb2.TypedValue(
                duration_value=duration_pb2.Duration(seconds=1, nanos=500000000)
            ),
            types_pb2.TypedValue(
                color_value=types_pb2.Color(set=True, red=1, green=2, blue=3)
            ),
            types_pb2.TypedValue(any_json=b'{"x":1}'),
        ]
        seen = set()
        for tv in cases:
            parsed = self.round_trip(tv)
            seen.add(parsed.WhichOneof("kind"))
        self.assertEqual(len(seen), 7, "one oneof case per propKinds row")

    def test_color_unset_distinguishable_from_black(self):
        unset = types_pb2.Color()
        black = types_pb2.Color(set=True)
        self.assertNotEqual(
            unset.SerializeToString(deterministic=True),
            black.SerializeToString(deterministic=True),
        )

    def test_request_messages_exist(self):
        control_pb2.SetPropertyRequest(
            name="Counter", value=types_pb2.TypedValue(int_value=7)
        )
        session_pb2.AttachRequest(
            subscribe=session_pb2.Subscription(frames=True)
        )

    def test_registration_crud_pair_exists(self):
        # Both halves, on both transports: a client that can grow the
        # binding context must be able to shrink it again.
        control_pb2.RegisterPropertiesRequest(
            properties=[
                types_pb2.PropertyRegistration(
                    name="Fresh", kind=types_pb2.ValueKind.VALUE_KIND_STRING
                )
            ]
        )
        control_pb2.UnregisterNamesRequest(names=["Fresh"])
        session_pb2.Act(
            id=1, unregister_names=control_pb2.UnregisterNamesRequest(names=["Fresh"])
        )

    def test_service_stubs_exist(self):
        self.assertTrue(hasattr(control_pb2_grpc, "ControlServiceStub"))
        self.assertTrue(hasattr(session_pb2_grpc, "SessionServiceStub"))
        self.assertTrue(hasattr(control_pb2_grpc.ControlServiceStub, "__init__"))
        self.assertIn("UnregisterNames", control_pb2_grpc.ControlServiceServicer.__dict__)


if __name__ == "__main__":
    unittest.main()
