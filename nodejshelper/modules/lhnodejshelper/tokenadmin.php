<?php

erLhcoreClassRestAPIHandler::setHeaders();

if (erLhcoreClassUser::instance()->isLogged() && erLhcoreClassUser::instance()->hasAccessTo('lhchat', 'use')) {
    $date = time();
    echo json_encode(sha1($date . 'Operator' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash')) . '.' . $date);
} else {
    echo json_encode("invalid");
}

exit;

?>