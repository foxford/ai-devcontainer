function TestProvider({ children }) { return <Provider>{children}</Provider>; }
function Fixture() { return <TestProvider><Card /></TestProvider>; }
