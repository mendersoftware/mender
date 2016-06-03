Overview of Mender client architecture.

```

                                  +--------------------+
                                  |       SERVER       |
                                  |                    |
                                  +-------^---+--------+
                                          |   |
                                          |   |
                                          |   |
         +------------------+     +-------+---v--------+     +-------------------+
         |      daemon      |     |      client        |     |      mender       |
         |                  |     |                    |     |                   |
         +------------------+     +--------------------+     +-------------------+
         |                  |     | Updater            |     | Controller        |
         |                  |     |                    |     |                   |
         +------------------+     +--------------------+     +-------------------+
         | NewDaemon        |     | NewUpdater         |     | NewMender         |
         | Run              |     | NewHttpsClient     |     | GetState          |
         | StopDaemon       <-----+ NewHttpClient      |     | GetCurrentImageID |
         |                  |     | GetScheduledUpdate |     | LoadConfig        |
         |                  |     | FetchUptate        |     | GetUpdaterConfig  |
         |                  |     | Bootstrap          |     | GetDaemonConfig   |
         |                  |     |                    |     |                   |
         +-------------^-^--+     +--------------------+     +-----+---^---------+
                       | |                                         |   |
                       | |-----------------------------------------+   |     MENDER SERVER INTERFACE
                       |                                               |
+--------------------------------------------------------------------------------------------------+
                       |                                               |
                       |                                               |          HARDWARE INTERFACE
                       |                                               |
                       |                                               |
                       |                                               |
+------------------+   |   +------------------------+        +---------+----------+
|    partitions    |   |   |         device         |        |      bootenv       |
|                  |   |   |                        |        |                    |
+------------------+   |   +------------------------+        +--------------------+
|                  |   |   | UInstaller             |        | BootEnvReadWritter |
|                  |   |   | UInstalCommitRebooter  |        |                    |
+------------------+   |   +------------------------+        +--------------------+
| GetInactive      |   |   | NewDevice              |        | NewEnvironment     |
| GetActive        |   +---+ Reboot                 |        | ReadEnv            |
|                  |       | InstallUpdate          <--------+ WriteEnv           |
|                  |       | EnableUpdatedPartition |        |                    |
|                  +-------> CommitUpdate           |        |                    |
|                  +-------> FetchUpdateFromFile    |        |                    |
|                  |       |                        |        |                    |
+------------------+       +------------------------+        +--------------------+

```
