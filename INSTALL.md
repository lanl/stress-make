Installing Stress Make
======================

Stress Make works by patching GNU Make to talk to a job-scheduling server.  You need a few things to build Stress Make:

1. A GNU Make tarball.  Download a recent one from http://ftpmirror.gnu.org/make/.

2. A compiler for the Go programming language.  Download the appropriate one for your platform from https://golang.org/dl/ and install it.

3. A C compiler (e.g., [GCC](https://gcc.gnu.org/)).

If you downloaded a Stress Make distribution that contains a top-level `configure` script, you should be good to go.  If not, you'll additionally need the GNU Autotools, specifically [Autoconf](https://www.gnu.org/software/autoconf/) and [Automake](https://www.gnu.org/software/automake/).  Install those, then generate a `configure` script&mdash;and other things needed by the build process&mdash;by running

    $ autoconf -f -v -i

(Ignore the `you should use literals` warnings.)

Once those prerequisites are satisfied you can configure, build, and install Stress Make.  You'll need to point `configure` to the GNU Make tarball you downloaded, though:

    $ ./configure --with-make-tar=$HOME/Downloads/make-4.1.tar.gz
    $ make
    $ make install

The default installation location is `/usr/local/`.  See [the Autoconf manual](https://www.gnu.org/savannah-checkouts/gnu/autoconf/manual/autoconf-2.69/html_node/Running-configure-Scripts.html) for instructions on how to tell `configure` to install into a different location, use nonstandard compiler options, and other such things.
