"""Tests for the runtime activity registry and the markup it generates.

unittest on purpose (no pytest dependency), matching clients/python/tests:

    ./.venv/bin/python -m unittest discover -s . -p 'test_*.py'
"""

import asyncio
import unittest

from registry import (
    ActivityError,
    Registry,
    activity_list_markup,
    reconcile_selection,
    result_property,
)

TITLECASE = 'def run(text):\n    return text.title()\n'


class RegistryTest(unittest.TestCase):
    def setUp(self) -> None:
        self.reg = Registry()

    def test_create_exec_and_invoke(self):
        act = self.reg.create("Titlecase", TITLECASE, "title-cases")
        self.assertEqual(act.name, "Titlecase")
        self.assertEqual(self.reg.names(), ["Titlecase"])
        got = asyncio.run(self.reg.invoke("Titlecase", ["hello there"]))
        self.assertEqual(got, "Hello There")

    def test_async_entry_point_is_awaited(self):
        self.reg.create("Slow", "async def run(text):\n    return text[::-1]\n")
        self.assertEqual(asyncio.run(self.reg.invoke("Slow", ["abc"])), "cba")

    def test_name_must_be_an_identifier(self):
        # The name is a Temporal activity type AND a gooey binding path
        # segment; anything else would generate markup that cannot parse.
        for bad in ["no-good", "1st", "has space", "dotted.name", ""]:
            with self.assertRaises(ActivityError, msg=bad):
                self.reg.create(bad, TITLECASE)

    def test_source_must_define_run(self):
        with self.assertRaises(ActivityError) as cm:
            self.reg.create("NoEntry", "x = 1\n")
        self.assertIn("run()", str(cm.exception))

    def test_syntax_error_names_the_activity(self):
        with self.assertRaises(ActivityError) as cm:
            self.reg.create("Broken", "def run(text:\n")
        self.assertIn("Broken", str(cm.exception))

    def test_duplicate_is_refused(self):
        self.reg.create("Titlecase", TITLECASE)
        with self.assertRaises(ActivityError):
            self.reg.create("Titlecase", TITLECASE)

    def test_delete_makes_it_unknown(self):
        self.reg.create("Titlecase", TITLECASE)
        self.reg.delete("Titlecase")
        self.assertEqual(self.reg.names(), [])
        with self.assertRaises(ActivityError):
            asyncio.run(self.reg.invoke("Titlecase", ["x"]))


class MarkupTest(unittest.TestCase):
    def test_empty_registry_still_yields_a_valid_fragment(self):
        src = activity_list_markup(Registry())
        # patch_markup requires the fragment root to carry the same Name
        # as the element it replaces, or the address is lost.
        self.assertIn('Name="ActivityList"', src)
        self.assertIn("<Gooey", src)

    def test_row_binds_the_registered_result_property(self):
        reg = Registry()
        reg.create("Titlecase", TITLECASE, "title-cases")
        src = activity_list_markup(reg)
        prop = result_property("Titlecase")
        self.assertEqual(prop, "Activity.Titlecase.Result")
        # The button binds the activity call by LITERAL name and delivers
        # into the property create_activity registered first — which is
        # what makes registration load-bearing rather than bookkeeping.
        self.assertIn(
            "Click=\"{{temporal:Activity `Titlecase` .Input | into .Activity.Titlecase.Result}}\"",
            src,
        )
        self.assertIn("{{.Activity.Titlecase.Result}}", src)
        # The fragment declares the namespace itself: an xmlns table is
        # per-document, so a patch cannot borrow the page's prefix.
        self.assertIn('xmlns:temporal="gooey.dev/handlers/temporal"', src)

    def test_description_is_xml_escaped(self):
        reg = Registry()
        reg.create("Escaped", TITLECASE, 'a <b> & "c"')
        src = activity_list_markup(reg)
        self.assertIn("&lt;b&gt; &amp;", src)
        self.assertNotIn("<b>", src)


class ReconcileSelectionTest(unittest.TestCase):
    """The half of the state-sync story that does not need a live app.

    The whole reason the worker subscribes to properties is so it can do
    this instead of overwriting: the app moves Selected on its own (the
    page binds ctrl+n to a Cycle command), and a tool call must respect
    that unless it has a reason not to.
    """

    def test_explicit_selection_wins(self):
        self.assertEqual(reconcile_selection("Shout", ["Shout", "Titlecase"], "Titlecase"), "Titlecase")

    def test_a_live_selection_is_left_alone(self):
        # The user pressed ctrl+n; deleting some OTHER activity must not
        # yank the selection back to whatever this worker last wrote.
        self.assertEqual(reconcile_selection("Titlecase", ["Shout", "Titlecase"]), "Titlecase")

    def test_a_dangling_selection_is_repointed(self):
        self.assertEqual(reconcile_selection("Deleted", ["Shout", "Titlecase"]), "Shout")

    def test_empty_registry_clears_the_selection(self):
        self.assertEqual(reconcile_selection("Titlecase", []), "")

    def test_empty_selection_takes_the_first(self):
        self.assertEqual(reconcile_selection("", ["Shout"]), "Shout")


class ControlModuleTest(unittest.TestCase):
    """Decoding, and the shape of the session surface.

    A real Attach stream needs a running app, which lives in the manual
    end-to-end path; what is worth pinning here is that the module keeps
    the streaming surface and does not quietly regrow a unary one.
    """

    def test_typed_value_decoding(self):
        from gooey.control.v1 import types_pb2

        from control import _plain

        self.assertEqual(_plain(types_pb2.TypedValue(string_value="hi")), "hi")
        self.assertEqual(_plain(types_pb2.TypedValue(int_value=7)), 7)
        self.assertIs(_plain(types_pb2.TypedValue(bool_value=True)), True)
        self.assertEqual(_plain(types_pb2.TypedValue(any_json=b'{"a":1}')), '{"a":1}')
        self.assertIsNone(_plain(types_pb2.TypedValue()))
        # An unhandled kind comes back as itself rather than as a wrong
        # string — a future propKinds row should look unhandled, not fine.
        color = types_pb2.TypedValue(color_value=types_pb2.Color(set=True, red=1))
        self.assertIsInstance(_plain(color), types_pb2.TypedValue)

    def test_the_session_is_stream_first(self):
        import inspect

        import control

        src = inspect.getsource(control.ControlSession)
        # Mutations are acts on the stream, not unary calls.
        for method in ("register_string", "unregister", "set_string"):
            self.assertIn(f"def {method}", src)
        self.assertIn("self.act(", src)
        # The two sanctioned unary calls, and no others: a one-shot
        # bootstrap read and PatchMarkup (which the Act oneof lacks).
        unary = [line.strip() for line in src.splitlines() if "self._unary." in line]
        self.assertEqual(len(unary), 2, unary)
        self.assertTrue(any("ListValues" in u for u in unary), unary)
        self.assertTrue(any("PatchMarkup" in u for u in unary), unary)


if __name__ == "__main__":
    unittest.main()
