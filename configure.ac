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
    [pristine make-VERSION.tar* file to untar and patch])],
  [make_tar="$withval"],
  [make_tar="make.tar.gz"])
AC_SUBST([WITH_MAKE_TAR], "--with-make-tar=$make_tar")

dnl Check for a few common programs we need configure and
dnl build stress-make.
AC_CHECK_PROGS([TAR], [tar], [tar])
AC_PROG_SED
AC_PROG_LN_S
AC_PROG_FGREP

dnl We require go proper (not even gccgo) to build stress-make.
AC_CHECK_PROGS([GO], [go], [no])
if test "$GO" = no ; then
  AC_MSG_ERROR([stress-make requires the `go' tool])
fi

dnl Ensure the GNU Make tarball exists and looks reasonable.
AC_MSG_CHECKING([make source tarball option])
if ! test -e "$make_tar" ; then
  AC_MSG_RESULT([not found])
  AC_MSG_ERROR([Specify --with-make-tar to point configure to make-*.tar.gz])
fi
AC_MSG_RESULT(["$make_tar"])

case $make_tar in
    *.tar.gz|*.tgz)
        tarflag="z"
        ;;
    *.tar.xz|*.txz)
        tarflags="J"
        ;;
    *.tar.bz2|*.tbz2)
        tarflags="j"
        ;;
    *.tar.lz)
        tarflags="--lzip"
        ;;
    *)
        tarflags=""
        ;;
esac
gmakedir=`$TAR -t${tarflag}f "$make_tar" | $SED 's,/.*,,' | uniq | head -1`

AC_MSG_CHECKING([make source tarball '$make_tar'])
if test -z "$gmakedir" ; then
  AC_MSG_RESULT([error])
  AC_MSG_ERROR([Specify --with-make-tar to point configure to a valid make-*.tar.gz])
fi
AC_MSG_RESULT([ok])
AC_SUBST([GMAKEDIR], [$gmakedir])

if test -e "$gmakedir/src"; then
    gmake_src_dir="$gmakedir/src"
else
    gmake_src_dir="$gmakedir"
fi

dnl Untar and patch GNU Make.
AS_ECHO_N(["extracting and patching $gmakedir from $make_tar... "])
rm -rf "$gmakedir"
$TAR -x${tarflag}f "$make_tar"
cp "$srcdir/remote-stress.c" "$gmake_src_dir/remote-stub.c"
# Create a configure.gnu file.  This requires
# temporarily expanding $prefix and $exec_prefix.
old_prefix="$prefix"
old_exec_prefix="$exec_prefix"
test "x$prefix" = xNONE && prefix="$ac_default_prefix"
test "x$exec_prefix" = xNONE && exec_prefix='${prefix}'
pkglibexecdir="${libexecdir}/${PACKAGE_NAME}"
while `echo "$pkglibexecdir" | $FGREP -q '$'` ; do
      pkglibexecdir="`eval echo $pkglibexecdir`"
done
prefix="$old_prefix"
exec_prefix="$old_exec_prefix"
gmake_configure_args=`echo "$ac_configure_args" | \
  $SED -e "s/'--\\(with-make-tar\\|srcdir\\|prefix\\|exec_prefix\\)=[[^']]*'//g"`
cat <<EOF > "$gmakedir/configure.gnu"
./configure $gmake_configure_args --prefix="$pkglibexecdir" --bindir="$pkglibexecdir/bin"
EOF
chmod +x "$gmakedir/configure.gnu"
# Suppress GNU Make tests, which will all fail due to "not run from
# stress-make" messages.
$SED -i -e '/^check-local:/s/check-regression//' "$gmakedir/Makefile.in"
# Effectively force "-j" for all compilations.
$SED -i -e 's/ job_slots = 1;/ job_slots = 0;/' "$gmake_src_dir/main.c"
# Identify make as having been patched.
$SED -i -e 's/ = VERSION;/ = VERSION "-stress";/' "$gmake_src_dir/version.c"
AS_ECHO([done])

dnl Determine if GNU Make's message() function requires 3 arguments (v4.1+)
dnl or 2 arguments (v4.0-).  We use grep because we haven't yet configured
dnl the GNU Make subdirectory.
AC_CACHE_CHECK([the minimum number of arguments to `message'],
  [ax_cv_func_message_args],
  [orig_CPPFLAGS="$CPPFLAGS"
   CPPFLAGS="$CPPFLAGS -I$gmake_src_dir"
   touch "$gmake_src_dir/config.h"
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
  AC_MSG_ERROR([failed to parse $gmake_src_dir/makeint.h])
fi
echo "#define NUM_MESSAGE_ARGS $ax_cv_func_message_args" > "$gmake_src_dir/remote-stress.h"

dnl Configure GNU Make.  AC_CONFIG_SUBDIRS([$gmakedir]) doesn't seem
dnl to work when we run "make distcheck".
AC_MSG_NOTICE([Configuring GNU Make])
(cd "$gmakedir" && ./configure.gnu)

dnl Generate our own Makefile.
AC_CONFIG_FILES([Makefile])
AC_OUTPUT
