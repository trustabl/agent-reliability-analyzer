- Register a context manager before anything else. Without one there is no
  current span inside a tool, and each label call does nothing.
- Build the `Run` and the attempt counts once per run, inside one function.
