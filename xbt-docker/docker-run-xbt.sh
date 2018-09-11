#!/bin/bash -ex

mysql=( mysql -h xbtdocker_mysql_1 -u xbt -pxbt xbt )
# until "${mysql[@]}" -e "select 1"; do
until "${mysql[@]}" -e "select 1" &>/dev/null ; do
  echo "MySQL is not ready yet..."
  sleep 1
done
echo "MySQL is ready."

cd /xbt/Tracker

mysql -h xbtdocker_mysql_1 -u xbt -pxbt xbt < xbt_tracker.sql &>/dev/null

./xbt_tracker
sleep infinity
