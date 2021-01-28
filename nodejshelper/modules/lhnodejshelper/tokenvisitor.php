<?php

erLhcoreClassRestAPIHandler::setHeaders();

$chatId = $Params['user_parameters']['chat_id'];

$date = time();

if (is_numeric($chatId)) {

    $chat = erLhcoreClassModelChat::fetch($chatId);

    if ($chat instanceof erLhcoreClassModelChat && $chat->hash == $Params['user_parameters']['hash'] && $chat->status !== erLhcoreClassModelChat::STATUS_CLOSED_CHAT) {
        echo json_encode(sha1($date . 'Visitor' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash') .'_' . $chatId) . '.' . $date);
    } else {
        echo json_encode("invalid");
    }

} else {
    echo json_encode(sha1($date . 'Visitor' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash')) . '.' . $date);
}

exit;

?>