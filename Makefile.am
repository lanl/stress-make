###################################
# Build a modified GNU Make that  #
# stress-tests Makefiles	  #
#				  #
# By Scott Pakin <pakin@lanl.gov> #
###################################

EXTRA_DIST = remote-stress.c
customized_make_dir = $(pkglibexecdir)/bin

$(GMAKEDIR): $(GMAKETAR) remote-stress.c
	$(TAR) -xaf $(GMAKETAR)
	cd $(GMAKEDIR) && \
	  cp $(abs_srcdir)/remote-stress.c remote-stubs.c && \
	  ./configure $(CONFIGURE_COMMAND_LINE) \
	    --prefix="$(pkglibexecdir)" \
	    --srcdir="$(abs_builddir)/$(GMAKEDIR)" \
	    --bindir="$(pkglibexecdir)/bin" && \
	  $(MAKE)

BUILT_SOURCES = $(GMAKEDIR) stress_make_SOURCES_builddir

bin_PROGRAMS = stress-make
stress_make_SOURCES = stress-make.go make-stats.go
nodist_stress_make_SOURCES = make-location.go

GO_VERBOSE = $(GO_VERBOSE_@AM_V@)
GO_VERBOSE_ = $(GO_VERBOSE_@AM_DEFAULT_V@)
GO_VERBOSE_0 = @echo GO $@;

stress_make_SOURCES_builddir: $(stress_make_SOURCES)
	test "$(abs_builddir)" == $(abs_srcdir) || $(LN_S) -f $(addprefix $(srcdir)/, $(stress_make_SOURCES)) $(builddir)

.PHONY: stress_make_SOURCES_builddir

stress-make$(EXEEXT): $(stress_make_SOURCES) $(nodist_stress_make_SOURCES)
	$(GO_VERBOSE) $(GO) build $(stress_make_SOURCES) $(nodist_stress_make_SOURCES)

make-location.go:
	echo 'package main' > make-location.go
	echo "const makeExecutable = \"$(customized_make_dir)/make\"" >> make-location.go

install-exec-hook: $(GMAKEDIR)
	cd $(GMAKEDIR) && \
	  $(MAKE) install
	cd $(DESTDIR)$(customized_make_dir) && \
	  $(LN_S) -f make gmake && \
	  $(LN_S) -f make gnumake

CLEANFILES = make-location.go

clean-local:
	$(RM) -r $(GMAKEDIR)

uninstall-local:
	$(RM) -r $(DESTDIR)$(pkglibexecdir)
