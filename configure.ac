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
AC_PROG_SED
AC_PROG_LN_S
AC_CHECK_PROGS([TAR], [tar], [tar])

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
AC_SUBST([GMAKETAR], [$make_tar])
AC_SUBST([GMAKEDIR], [$gmakedir])

dnl Generate a Makefile.
AC_CONFIG_FILES([Makefile])
AC_OUTPUT
