#!/bin/sh

if [ ! -d '/tiles/statistics/' ]; then
    # Run Sourcing && null tilegen
    ./get_static_data.sh && ./tilegen.sh
fi;

# Start Cron && run tilegen.sh however often...
/usr/sbin/crond -f -l 8