<?php

erLhcoreClassRestAPIHandler::setHeaders();

$chatId = $Params['user_parameters']['chat_id'];

$date = time();

if (is_numeric($chatId)) {
    echo json_encode(sha1($date . 'Visitor' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash') .'_' . $chatId) . '.' . $date);
} else {
    echo json_encode(sha1($date . 'Visitor' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash')) . '.' . $date);
}

exit;

?>