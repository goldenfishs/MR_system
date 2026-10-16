FROM judge0/judge0:1.13.1

USER root
# Keep the worker's environment file without printing its secrets to the log.
# Fail the build if the upstream script no longer matches this narrow patch.
RUN sed -i 's@^\([[:space:]]*export | sudo tee /api/environment\)$@\1 > /dev/null@' /api/scripts/workers \
    && grep -Eq '^[[:space:]]*export \| sudo tee /api/environment > /dev/null$' /api/scripts/workers
USER judge0
