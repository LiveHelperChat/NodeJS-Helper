<?php

return array(
    'connect_db' => 'localhost',
    'connect_db_id' => 0,
    'automated_hosting' => false,
    'public_settings' => array(
        'hostname' => (isset($_SERVER['HTTP_HOST']) ? $_SERVER['HTTP_HOST'] : null),
        'path' => '/socketcluster/',
        'port' => null, //some custom port
        'secure' => null, // true || false
    )
);

?>