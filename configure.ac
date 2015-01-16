dnl -----------------------------------------
dnl Configure stress-make before building it
dnl
dnl By Scott Pakin <pakin@lanl.gov>
dnl -----------------------------------------

dnl Initialize the configure script.
AC_PREREQ([2.69])
AC_INIT([stress-make], [1.0], [pakin@lanl.gov])
AC_CONFIG_SRCDIR([remote-stress.c])
AM_INIT_AUTOMAKE([foreign])

dnl Find a GNU Make tarball we can patch.
AC_ARG_WITH([make-tar],
  [AS_HELP_STRING([--with-make-tar],
    [pristine make-VERSION.tar.gz file to untar and patch])],
  [make_tar="$withval"],
  [make_tar="make.tar.gz"])

dnl Export our command line to the Makefile.
AC_SUBST([CONFIGURE_COMMAND_LINE], [$ac_configure_args])

dnl Check for a few common programs we need configure and
dnl build stress-make.
AC_CHECK_PROGS([TAR], [tar], [tar])
AC_PROG_SED
AC_PROG_LN_S

dnl We require go proper (not even gccgo) to build stress-make.
AC_CHECK_PROGS([GO], [go], [no])
if test "$GO" = no ; then
  AC_MSG_ERROR([stress-make requires the `go' tool])
fi

dnl Ensure the GNU Make tarball exists and looks reasonable.
AC_MSG_CHECKING([$make_tar])
if ! test -e "$make_tar" ; then
  AC_MSG_RESULT([not found])
  AC_MSG_ERROR([Specify --with-make-tar to point configure to make-*.tar.gz])
fi
gmakedir=`$TAR -taf "$make_tar" | $SED 's,/.*,,' | uniq | head -1`
if test -z "$gmakedir" ; then
  AC_MSG_RESULT([error])
  AC_MSG_ERROR([Specify --with-make-tar to point configure to a valid make-*.tar.gz])
fi
AC_MSG_RESULT([ok])
AC_SUBST([GMAKEDIR], [$gmakedir])

dnl Untar and patch GNU Make.
AC_MSG_NOTICE([Extracting and patching $make_tar])
$TAR -xaf "$make_tar"
cp "$srcdir/remote-stress.c" "$gmakedir/remote-stub.c"
cnf_prefix="$prefix"
cnf_exec_prefix="$exec_prefix"
test "x$cnf_prefix" = xNONE && cnf_prefix=$ac_default_prefix
test "x$cnf_exec_prefix" = xNONE && cnf_exec_prefix='${prefix}'
pkglibexecdir="${libexecdir}/${PACKAGE_NAME}"
cat <<EOF > "$gmakedir/configure.gnu"
prefix="$cnf_prefix"
exec_prefix="$cnf_exec_prefix"
./configure $CONFIGURE_COMMAND_LINE --prefix="$pkglibexecdir" --bindir="$pkglibexecdir/bin"
EOF
dnl Disable GNU Make tests, which will all fail due to "not run from
dnl stress-make" messages.
$SED -i -e '/^check-local:/s/check-regression//' "$gmakedir/Makefile.in"

dnl Determine if GNU Make's message() function requires 3 arguments (v4.1+)
dnl or 2 arguments (v4.0-).  We use grep because we haven't yet configured
dnl the GNU Make subdirectory.
AC_CACHE_CHECK([the minimum number of arguments to `message'],
  [ax_cv_func_message_args],
  [orig_CPPFLAGS="$CPPFLAGS"
   CPPFLAGS="$CPPFLAGS -I$gmakedir"
   touch "$gmakedir/config.h"
   AC_EGREP_HEADER(
    [^void message.*prefix.*length.*fmt],
    [makeint.h],
    [ax_cv_func_message_args=3],
    [AC_EGREP_HEADER(
      [^void message.*prefix.*fmt],
      [makeint.h],
      [ax_cv_func_message_args=2],
      [ax_cv_func_message_args=unknown])])
   CPPFLAGS="$orig_CPPFLAGS"])
if test "x$ax_cv_func_message_args" = xunknown ; then
  AC_MSG_ERROR([failed to parse $gmakedir/makeint.h])
fi
echo "#define NUM_MESSAGE_ARGS $ax_cv_func_message_args" > "$gmakedir/remote-stress.h"

dnl Configure GNU Make and generate our own Makefile.
AC_CONFIG_SUBDIRS([$gmakedir])
AC_CONFIG_FILES([Makefile])
AC_OUTPUT
