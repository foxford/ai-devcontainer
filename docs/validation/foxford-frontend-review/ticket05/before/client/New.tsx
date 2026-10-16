import React from "react";
import { useIntl } from "react-intl";
export function New() { const intl = useIntl(); return <aside>{intl.formatMessage({ id: "new", defaultMessage: "New" })}</aside>; }
