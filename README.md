Stress Make
===========

Description
-----------

Stress Make helps developers expose race conditions in their `Makefile`s.

[GNU Make](http://www.gnu.org/software/make/) supports a `-j` (or `--jobs`) option that allows multiple jobs (commands) to run in parallel.  However, it is the developer's responsibility to ensure that dependencies among rules are specified correctly.  One problem that arises is that an incorrect `Makefile` may build correctly on one system even if it fails on another system simple due to the order that the jobs happen to run.

Stress Make is a customized GNU Make that explicitly manages the order in which concurrent jobs are run in order to provoke erroneous behavior into becoming manifest.  It can run jobs in the order they're launched, in backwards order, or in random order.  The thought is that if code builds correctly with Stress Make then it is likely (but not guaranteed) that the `Makefile` contains no race conditions.

Installation
------------

See [INSTALL.md](https://github.com/lanl/stress-make/blob/master/INSTALL.md) for installation instructions.

Usage
-----

Simple replace `make` with `stress-make` when building code.  Stress Make will always enable unbounded concurrency (the equivalent of `-j` with no argument) but will serialize actual execution so there's no risk of bogging down your machine with hundreds of processes fighting for the CPU.  Optionally, you can specify `--order=random` to randomize the order in which processes execute.  (There's also `--order=fifo`, but that's the default for `make` so it's not particularly interesting from a stress-testing perspective.)

When the build is complete, Stress Make outputs some information about the build.  For example, it outputs the following when building itself:

	stress-make: INFO: Total commands launched = 71
	stress-make: INFO: Maximum concurrency observed = 28
	stress-make: INFO: Time on a single CPU core (T_1) = 1.156269146s
	stress-make: INFO: Time on infinite CPU cores (T_inf) = 542.823888ms
	stress-make: INFO: Maximum possible speedup (T_1/T_inf) = 2.130100

That is, the build launched a total of 71 jobs.  At most 28 of these were enqueued or running at the same time.  Excluding queue delays and Stress Make overhead, the build would have finished in a little over a second if executed serially.  (This is known as <q>work</q> or <i>T</i><sub>1</sub>.)  Given an infinite number of CPUs, the build could have completed in a little over half a second.  (This is known as <q>span</q> or <i>T</i><sub>&infin;</sub>.)  Hence, the `Makefile` offers only enough concurrency to support a maximum speedup of 2x no matter how many CPUs you throw at it.

See Charles Leiserson's <q>[What the $#@! is Parallelism, Anyhow?](https://www.cprogramming.com/parallelism.html)</q> article for a nice discussion of work, span, parallelism, and related concepts.

Copyright and license
---------------------

Triad National Security, LLC (Triad) owns the copyright to Stress Make, which it identifies internally as LA-CC-11-056.  The license is BSD-ish with a "modifications must be indicated" clause.  See [LICENSE.md](https://github.com/lanl/stress-make/blob/master/LICENSE.md) for the full text.

Author
------

Scott Pakin, [_pakin@lanl.gov_](mailto:pakin@lanl.gov)
