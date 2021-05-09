"""
Write-behind pattern for Goal 3: Static, Historical Layers
of transit performance
"""
import json
import os
import sys
import uuid

import psycopg2
import psycopg2.extras
from psycopg2 import sql


class PostgresConnector:
    """Custom Connector for RedisGears -> PostgreSQL"""

    def __init__(
        self, table_name, user="postgres", db="postgres", host="localhost", port=5432
    ):
        self.table_name = table_name
        self._user = user
        self._db = db
        self._host = host
        self._port = port
        self.connection = None
        
    def connect(self):
        """Wrapper for Connecting to PostgreSQL Instance"""
        c = psycopg2.connect(
            host=self._host,
            dbname=self._db,
            user=self._user,
            password=os.environ.get("PGPASSWORD", ""),
            port=self._port,
        )
        return c

    def pg_write_behind(self, data):
        """
        Implement WriteBehind pattern, given some data formatted as a list of dict,
        write the contents of the dict's `value` field to Postgres
        """
        if len(data) == 0:
            return

        if not self.connection:
            self.connection = self.connect()

        ## Extract `value` field from each response body, ignores any other keys,
        # i.e. `ID` Redis generates and passes along. Across multiple shards these
        # IDs can't be trusted to be unique, esp. during spikes in traffic...
        events_list = (json.dumps(e.get("value", "")) for e in data)

        with self.connection.cursor() as cur:

            # Use approx time for all batches to avoid json read...
            
            schema, tbl = self.table_name.split('.')

            # NOTE (04/23/2021): For the moment this is dumping value as a JSONB, to improve
            # performance a teensy bit, prepare the insert
            prepared_insert = sql.SQL(
                """PREPARE stmt (varchar, JSONB) AS INSERT INTO {schema}.{table} (id, body) VALUES ($1::UUID, $2)"""
            ).format(
                schema=sql.Identifier(schema),
                table=sql.Identifier(tbl)
            )

            cur.execute(prepared_insert)

            # Execute the prepared request in batches up to [size], this value can be tuned
            # based on the memory available in your system, even though this runs off the
            # main thread, if this batch is too large, Redis throughput drops
            psycopg2.extras.execute_batch(
                cur,
                "EXECUTE stmt (%s, %s)",
                map(lambda x: (uuid.uuid4().__str__(), x), events_list),
                page_size=10000,  # NOTE: Tune this Parameter....
            )

            # Execute explicit deallocate
            cur.execute("DEALLOCATE stmt")

        self.connection.commit()


# Write Raw Data to Postgres....
positions_connector = PostgresConnector(
    table_name="statistics.events", host="postgis"
)

sreader = GearsBuilder(
    "StreamReader",
    defaultArg="events",
    desc=json.dumps(
        {
            "name": "events.StreamReader",
            "desc": "Read from the all events stream and write to DB table",
        }
    ),
)

# Gather the contents of multiple streams to a single list, can be
# dangerous if trying to gather too many objects, local and global
# gather functions run back-to-back...
# NOTE: Tune this for your system!!!
sreader.aggregate([], lambda a, r: a + [r], lambda a, r: a + r)

# Only one observation here given the previous gather, expect a single
# call of write_behind()
sreader.foreach(lambda d: positions_connector.pg_write_behind(d))

# Count held as a dummy statement here to suppress response when testing
# locally and as a check on writes, should always yield "1" as result
sreader.count()


# NOTE: Tune duration/interval and batch size parameters for your system!!!
sreader.register(
    fromId="0-0", duration=5000, batch=10000, trimStream=True, mode="async"
)

