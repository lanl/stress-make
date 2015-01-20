###################################
# Build a modified GNU Make that  #
# stress-tests Makefiles	  #
#				  #
# By Scott Pakin <pakin@lanl.gov> #
###################################

SUBDIRS = $(GMAKEDIR)
EXTRA_DIST = remote-stress.c README.md INSTALL.md COPYING.md
gmakebindir = $(pkglibexecdir)/bin
AM_DISTCHECK_CONFIGURE_FLAGS = $(WITH_MAKE_TAR)

bin_PROGRAMS = stress-make
stress_make_SOURCES = stress-make.go make-stats.go
nodist_stress_make_SOURCES = make-location.go
dist_man1_MANS = stress-make.1

GO_VERBOSE = $(GO_VERBOSE_@AM_V@)
GO_VERBOSE_ = $(GO_VERBOSE_@AM_DEFAULT_V@)
GO_VERBOSE_0 = @echo GO $@;

BUILT_SOURCES = stress_make_SOURCES_builddir

stress_make_SOURCES_builddir: $(stress_make_SOURCES)
	test "$(abs_builddir)" == $(abs_srcdir) || $(LN_S) -f $(addprefix $(srcdir)/, $(stress_make_SOURCES)) $(builddir)

.PHONY: stress_make_SOURCES_builddir

stress-make$(EXEEXT): $(stress_make_SOURCES) $(nodist_stress_make_SOURCES)
	$(GO_VERBOSE) $(GO) build $(stress_make_SOURCES) $(nodist_stress_make_SOURCES)

make-location.go:
	echo 'package main' > make-location.go
	echo "const makeExecutable = \"$(gmakebindir)/make\"" >> make-location.go

install-exec-hook:
	$(INSTALL) -d "$(DESTDIR)$(gmakebindir)"
	$(LN_S) -f make "$(DESTDIR)$(gmakebindir)/gmake"
	$(LN_S) -f make "$(DESTDIR)$(gmakebindir)/gnumake"

CLEANFILES = make-location.go

distclean-local:
	$(RM) -r $(GMAKEDIR)

uninstall-local:
	$(RM) -r $(DESTDIR)$(pkglibexecdir)

dist-hook:
	chmod -R u+rwX $(distdir)/$(GMAKEDIR)
	$(RM) -r  $(distdir)/$(GMAKEDIR)
